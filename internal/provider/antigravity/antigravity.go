// Package antigravity implements domain.SessionProvider for Google's
// Antigravity CLI (agy), which stores one directory per conversation under
// ~/.gemini/antigravity-cli/brain/<uuid>/ with the transcript at
// <uuid>/.system_generated/logs/transcript.jsonl. There is no metadata file,
// so titles/cwd are unavailable and timestamps come from the transcript mtime.
package antigravity

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/semigodking/clisesh/internal/domain"
	"github.com/semigodking/clisesh/internal/provider"
)

const providerID = "antigravity"

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func init() {
	provider.Register(providerID, func(baseDir string) (domain.SessionProvider, error) {
		return New(baseDir), nil
	})
}

// Provider is the Antigravity session provider.
type Provider struct {
	dir         string // the brain/ directory
	presenceDir string // the presence/ directory holding <id>.lock files
}

// New returns an Antigravity provider. If baseDir is empty, the default
// ~/.gemini/antigravity-cli/brain is used.
func New(baseDir string) *Provider {
	if baseDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		baseDir = filepath.Join(home, ".gemini", "antigravity-cli", "brain")
	}
	presenceDir := filepath.Join(filepath.Dir(baseDir), "presence")
	return &Provider{
		dir:         baseDir,
		presenceDir: presenceDir,
	}
}

// ID returns the provider identifier.
func (p *Provider) ID() string { return providerID }

// Capabilities reports Antigravity's feature set: no titles, no parent links,
// no cascade delete; resume/delete/prompts supported.
func (p *Provider) Capabilities() domain.Capabilities {
	return domain.Capabilities{
		HasTitle:        false,
		HasParent:       false,
		CanResume:       true,
		CanDelete:       true,
		CascadeDelete:   false,
		ProvidesPrompts: true,
	}
}

// transcriptPath returns the transcript file path for a conversation id.
func (p *Provider) transcriptPath(id string) string {
	return filepath.Join(p.dir, id, ".system_generated", "logs", "transcript.jsonl")
}

// isLocked reports whether the conversation is currently held by an active agy process.
// Antigravity holds a BSD flock (LOCK_EX) on ~/.gemini/antigravity-cli/presence/<id>.lock.
func (p *Provider) isLocked(id string) bool {
	lockPath := filepath.Join(p.presenceDir, id+".lock")
	f, err := os.OpenFile(lockPath, os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer f.Close()

	// Attempt a non-blocking exclusive flock. If another process holds the lock,
	// flock returns EWOULDBLOCK / EAGAIN.
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return true
		}
		return false
	}

	// We acquired the lock, so no active process is using it. Release immediately.
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	return false
}

// List returns conversations newest-first (by transcript mtime).
func (p *Provider) List(ctx context.Context) ([]domain.Session, error) {
	entries, err := os.ReadDir(p.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var sessions []domain.Session
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id := e.Name()
		if !uuidRe.MatchString(id) {
			continue
		}
		updated := p.updatedAt(id, e)
		sessions = append(sessions, domain.Session{
			ID:         id,
			ProviderID: providerID,
			UpdatedAt:  updated,
			Locked:     p.isLocked(id),
		})
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})
	return sessions, nil
}

// updatedAt returns the transcript mtime, falling back to the dir mtime.
func (p *Provider) updatedAt(id string, dirEntry os.DirEntry) time.Time {
	if fi, err := os.Stat(p.transcriptPath(id)); err == nil {
		return fi.ModTime().UTC()
	}
	if info, err := dirEntry.Info(); err == nil {
		return info.ModTime().UTC()
	}
	return time.Time{}
}

// transcriptEntry is a defensively-typed view of a transcript line.
type transcriptEntry struct {
	Type    string          `json:"type"`
	Text    string          `json:"text"`
	Message json.RawMessage `json:"message"`
	Content json.RawMessage `json:"content"`
}

// UserPrompts extracts up to max USER_INPUT texts from the transcript.
func (p *Provider) UserPrompts(ctx context.Context, id string, max int) ([]string, error) {
	if !uuidRe.MatchString(id) {
		return nil, nil
	}
	f, err := os.Open(p.transcriptPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var prompts []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if !strings.Contains(string(line), "USER_INPUT") {
			continue
		}
		var e transcriptEntry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		if e.Type != "USER_INPUT" {
			continue
		}
		if t := extractText(e); t != "" {
			prompts = append(prompts, t)
			if max > 0 && len(prompts) >= max {
				break
			}
		}
	}
	return prompts, sc.Err()
}

// extractText pulls user text from the several shapes the transcript may use.
func extractText(e transcriptEntry) string {
	if t := strings.TrimSpace(e.Text); t != "" {
		return t
	}
	// Try message as a plain string, then as {text: "..."}.
	if s := rawString(e.Message); s != "" {
		return s
	}
	if s := rawTextField(e.Message); s != "" {
		return s
	}
	// Try content as a plain string or [{text: "..."}].
	if s := rawString(e.Content); s != "" {
		return s
	}
	if s := rawTextField(e.Content); s != "" {
		return s
	}
	return ""
}

func rawString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	return ""
}

func rawTextField(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Object with a text field.
	var obj struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.Text != "" {
		return strings.TrimSpace(obj.Text)
	}
	// Array of objects with text fields.
	var arr []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &arr); err == nil {
		var parts []string
		for _, a := range arr {
			if t := strings.TrimSpace(a.Text); t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

// ValidateResume checks if the session exists and agy executable is available on PATH.
func (p *Provider) ValidateResume(ctx context.Context, id string) error {
	if !uuidRe.MatchString(id) {
		return domain.ErrNotSupported
	}
	_, err := lookAgy()
	return err
}

// Resume replaces the current process with `agy --conversation <id>`.
func (p *Provider) Resume(ctx context.Context, id string) error {
	if !uuidRe.MatchString(id) {
		return domain.ErrNotSupported
	}
	bin, err := lookAgy()
	if err != nil {
		return err
	}
	return syscall.Exec(bin, []string{"agy", "--conversation", id}, os.Environ())
}

// Children returns nil — Antigravity has no parent/child linkage.
func (p *Provider) Children(ctx context.Context, id string) ([]string, error) {
	return nil, nil
}

// Delete removes conversation directories after strict id validation,
// and cleans up any lingering presence lock file.
func (p *Provider) Delete(ctx context.Context, ids []string) ([]string, error) {
	var deleted []string
	for _, id := range ids {
		if !safeID(id) {
			continue // never risk deleting the brain root or escaping it
		}
		path := filepath.Join(p.dir, id)
		if err := os.RemoveAll(path); err == nil {
			deleted = append(deleted, id)
			// Also clean up any lingering 0-byte presence lock file.
			lockPath := filepath.Join(p.presenceDir, id+".lock")
			_ = os.Remove(lockPath)
		}
	}
	sort.Strings(deleted)
	return deleted, nil
}

// safeID guards against path traversal and root deletion.
func safeID(id string) bool {
	if id == "" || id == "." || id == ".." {
		return false
	}
	if strings.ContainsAny(id, `/\`) {
		return false
	}
	return uuidRe.MatchString(id)
}
