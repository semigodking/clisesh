// Package kiro implements domain.SessionProvider for the Kiro CLI, which
// stores each session as <uuid>.json (metadata) + <uuid>.jsonl (transcript)
// + optional <uuid>.lock / <uuid>.history under ~/.kiro/sessions/cli/.
package kiro

import (
	"bufio"
	"context"
	"encoding/json"
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

const providerID = "kiro"

// uuidRe validates a canonical lowercase UUID (the session id / filename stem).
var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func init() {
	provider.Register(providerID, func(baseDir string) (domain.SessionProvider, error) {
		return New(baseDir), nil
	})
}

// Provider is the Kiro session provider.
type Provider struct {
	dir string // sessions directory
}

// New returns a Kiro provider. If baseDir is empty, the default
// ~/.kiro/sessions/cli is used.
func New(baseDir string) *Provider {
	if baseDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = "."
		}
		baseDir = filepath.Join(home, ".kiro", "sessions", "cli")
	}
	return &Provider{dir: baseDir}
}

// ID returns the provider identifier.
func (p *Provider) ID() string { return providerID }

// Capabilities reports Kiro's full feature set.
func (p *Provider) Capabilities() domain.Capabilities {
	return domain.Capabilities{
		HasTitle:        true,
		HasParent:       true,
		CanResume:       true,
		CanDelete:       true,
		CascadeDelete:   true,
		ProvidesPrompts: true,
	}
}

// kiroMeta is the subset of <uuid>.json we parse.
type kiroMeta struct {
	Title           string `json:"title"`
	CWD             string `json:"cwd"`
	UpdatedAt       string `json:"updated_at"`
	CreatedAt       string `json:"created_at"`
	ParentSessionID string `json:"parent_session_id"`
}

// List returns top-level (non-subagent) sessions, newest first.
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
		name := e.Name()
		if strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		if !uuidRe.MatchString(id) {
			continue
		}
		meta, err := p.readMeta(id)
		if err != nil {
			continue // skip unreadable/corrupt metadata
		}
		if meta.ParentSessionID != "" {
			continue // exclude subagent sessions
		}
		sessions = append(sessions, domain.Session{
			ID:         id,
			ProviderID: providerID,
			Title:      meta.Title,
			CWD:        meta.CWD,
			UpdatedAt:  parseTime(meta.UpdatedAt, meta.CreatedAt),
			Locked:     p.exists(id + ".lock"),
			ParentID:   meta.ParentSessionID,
		})
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})
	return sessions, nil
}

// UserPrompts extracts up to max user prompt texts from <id>.jsonl.
func (p *Provider) UserPrompts(ctx context.Context, id string, max int) ([]string, error) {
	if !uuidRe.MatchString(id) {
		return nil, nil
	}
	f, err := os.Open(filepath.Join(p.dir, id+".jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var prompts []string
	sc := bufio.NewScanner(f)
	// Allow very long lines (large tool results / assistant messages).
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		// Cheap pre-filter before JSON parsing.
		if !strings.Contains(string(line), `"kind":"Prompt"`) {
			continue
		}
		var entry struct {
			Kind string `json:"kind"`
			Data struct {
				Content []struct {
					Kind string `json:"kind"`
					Data string `json:"data"`
				} `json:"content"`
			} `json:"data"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if entry.Kind != "Prompt" {
			continue
		}
		var parts []string
		for _, c := range entry.Data.Content {
			if c.Kind == "text" {
				if t := strings.TrimSpace(c.Data); t != "" {
					parts = append(parts, t)
				}
			}
		}
		if len(parts) > 0 {
			prompts = append(prompts, strings.Join(parts, " "))
			if max > 0 && len(prompts) >= max {
				break
			}
		}
	}
	return prompts, sc.Err()
}

// ValidateResume checks if the session exists and kiro-cli executable is available on PATH.
func (p *Provider) ValidateResume(ctx context.Context, id string) error {
	if !uuidRe.MatchString(id) {
		return domain.ErrNotSupported
	}
	_, err := lookKiro()
	return err
}

// Resume replaces the current process with kiro-cli resuming the session.
func (p *Provider) Resume(ctx context.Context, id string) error {
	if !uuidRe.MatchString(id) {
		return domain.ErrNotSupported
	}
	bin, err := lookKiro()
	if err != nil {
		return err
	}
	args := []string{"kiro-cli", "chat", "--resume-id", id}
	return syscall.Exec(bin, args, os.Environ())
}

// Children returns IDs of subagent sessions whose parent is id.
func (p *Provider) Children(ctx context.Context, id string) ([]string, error) {
	entries, err := os.ReadDir(p.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var kids []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		childID := strings.TrimSuffix(name, ".json")
		if childID == id || !uuidRe.MatchString(childID) {
			continue
		}
		meta, err := p.readMeta(childID)
		if err != nil {
			continue
		}
		if meta.ParentSessionID == id {
			kids = append(kids, childID)
		}
	}
	sort.Strings(kids)
	return kids, nil
}

// Delete removes the given sessions and their subagent children.
func (p *Provider) Delete(ctx context.Context, ids []string) ([]string, error) {
	toDelete := map[string]bool{}
	for _, id := range ids {
		if !uuidRe.MatchString(id) {
			continue // never touch anything that is not a valid session id
		}
		toDelete[id] = true
		kids, _ := p.Children(ctx, id)
		for _, k := range kids {
			toDelete[k] = true
		}
	}
	var deleted []string
	for id := range toDelete {
		removedAny := false
		for _, ext := range []string{".json", ".jsonl", ".lock", ".history"} {
			path := filepath.Join(p.dir, id+ext)
			if err := os.Remove(path); err == nil {
				removedAny = true
			}
		}
		if removedAny {
			deleted = append(deleted, id)
		}
	}
	sort.Strings(deleted)
	return deleted, nil
}

// --- helpers ---

func (p *Provider) readMeta(id string) (kiroMeta, error) {
	var m kiroMeta
	data, err := os.ReadFile(filepath.Join(p.dir, id+".json"))
	if err != nil {
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, err
	}
	return m, nil
}

func (p *Provider) exists(name string) bool {
	_, err := os.Stat(filepath.Join(p.dir, name))
	return err == nil
}

// parseTime parses an ISO-8601 timestamp (updated preferred, else created).
func parseTime(updated, created string) time.Time {
	for _, s := range []string{updated, created} {
		if s == "" {
			continue
		}
		s = strings.Replace(s, "Z", "+00:00", 1)
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.UTC()
		}
		if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
			return t.UTC()
		}
	}
	return time.Time{}
}
