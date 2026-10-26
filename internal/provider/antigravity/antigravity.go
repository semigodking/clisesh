// Package antigravity implements domain.SessionProvider for Google's
// Antigravity CLI (agy), which stores conversation brains under
// ~/.gemini/antigravity-cli/brain/<uuid>/ with transcripts at
// <uuid>/.system_generated/logs/transcript.jsonl, and conversation SQLite
// databases under ~/.gemini/antigravity-cli/conversations/<uuid>.db.
// CWD is resolved from conversations/<uuid>.db, while prompts are read from
// the transcript JSONL.
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

var (
	uuidRe          = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	cwdRegex        = regexp.MustCompile(`(?:read_file|write_file)\((/[^)*?\x00-\x1f\r\n]+)\)`)
	cwdFileUriRegex = regexp.MustCompile(`file://(/[^)*?\x00-\x1f\r\n"'` + "`" + ` ]+)`)
)

func init() {
	provider.Register(providerID, func(baseDir string) (domain.SessionProvider, error) {
		return New(baseDir), nil
	})
}

// Provider is the Antigravity session provider.
type Provider struct {
	dir              string // the brain/ directory
	presenceDir      string // the presence/ directory holding <id>.lock files
	conversationsDir string // the conversations/ directory holding <id>.db files
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
	parent := filepath.Dir(baseDir)
	presenceDir := filepath.Join(parent, "presence")
	conversationsDir := filepath.Join(parent, "conversations")
	return &Provider{
		dir:              baseDir,
		presenceDir:      presenceDir,
		conversationsDir: conversationsDir,
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

// extractCWD extracts the workspace working directory from conversations/<id>.db.
func (p *Provider) extractCWD(id string) string {
	dbPath := filepath.Join(p.conversationsDir, id+".db")
	f, err := os.Open(dbPath)
	if err != nil {
		home, hErr := os.UserHomeDir()
		if hErr == nil {
			fallbackPath := filepath.Join(home, ".gemini", "antigravity-ide", "conversations", id+".db")
			f, err = os.Open(fallbackPath)
		}
	}
	if err != nil {
		return ""
	}
	defer f.Close()

	buf := make([]byte, 2*1024*1024)
	n, _ := f.Read(buf)
	if n <= 0 {
		return ""
	}
	data := buf[:n]

	// 1. Try read_file(...) or write_file(...) permission pattern
	if m := cwdRegex.FindSubmatch(data); len(m) > 1 {
		path := strings.TrimSpace(string(m[1]))
		if path != "" && path != "*" && filepath.IsAbs(path) {
			return filepath.Clean(path)
		}
	}

	// 2. Try file:///... URI pattern
	if m := cwdFileUriRegex.FindSubmatch(data); len(m) > 1 {
		path := strings.TrimSpace(string(m[1]))
		if path != "" && filepath.IsAbs(path) {
			return filepath.Clean(path)
		}
	}

	return ""
}

// List returns conversations newest-first.
func (p *Provider) List(ctx context.Context) ([]domain.Session, error) {
	idSet := make(map[string]bool)

	// Collect from brain/ directory
	if entries, err := os.ReadDir(p.dir); err == nil {
		for _, e := range entries {
			if e.IsDir() && uuidRe.MatchString(e.Name()) {
				idSet[e.Name()] = true
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	// Collect from conversations/ directory (<uuid>.db)
	if entries, err := os.ReadDir(p.conversationsDir); err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".db") {
				id := strings.TrimSuffix(e.Name(), ".db")
				if uuidRe.MatchString(id) {
					idSet[id] = true
				}
			}
		}
	}

	var sessions []domain.Session
	for id := range idSet {
		updated := p.updatedAt(id)
		cwd := p.extractCWD(id)
		sessions = append(sessions, domain.Session{
			ID:         id,
			ProviderID: providerID,
			CWD:        cwd,
			UpdatedAt:  updated,
			Locked:     p.isLocked(id),
		})
	}

	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})
	return sessions, nil
}

// updatedAt returns the newest timestamp across transcript, brain dir, and db file.
func (p *Provider) updatedAt(id string) time.Time {
	var newest time.Time

	// 1. transcript mtime
	if fi, err := os.Stat(p.transcriptPath(id)); err == nil {
		if fi.ModTime().After(newest) {
			newest = fi.ModTime().UTC()
		}
	}
	// 2. brain/<id> dir mtime
	if fi, err := os.Stat(filepath.Join(p.dir, id)); err == nil {
		if fi.ModTime().After(newest) {
			newest = fi.ModTime().UTC()
		}
	}
	// 3. conversations/<id>.db mtime
	if fi, err := os.Stat(filepath.Join(p.conversationsDir, id+".db")); err == nil {
		if fi.ModTime().After(newest) {
			newest = fi.ModTime().UTC()
		}
	}

	return newest
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

// Delete removes conversation directories and database files after strict id validation,
// and cleans up any lingering presence lock file.
func (p *Provider) Delete(ctx context.Context, ids []string) ([]string, error) {
	var deleted []string
	for _, id := range ids {
		if !safeID(id) {
			continue // never risk deleting the brain root or escaping it
		}
		path := filepath.Join(p.dir, id)
		dbPath := filepath.Join(p.conversationsDir, id+".db")
		lockPath := filepath.Join(p.presenceDir, id+".lock")

		_ = os.RemoveAll(path)
		_ = os.Remove(dbPath)
		_ = os.Remove(lockPath)
		deleted = append(deleted, id)
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
