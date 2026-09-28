// Package codex implements domain.SessionProvider for OpenAI's Codex CLI.
// Sessions are stored as JSONL rollout files under ~/.codex/sessions/ (or $CODEX_HOME/sessions/),
// typically organized in date-based subdirectories (e.g. sessions/YYYY/MM/DD/rollout-*.jsonl or <timestamp>.<id>.jsonl).
package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/semigodking/clisesh/internal/domain"
	"github.com/semigodking/clisesh/internal/provider"
)

const providerID = "codex"

// uuidRe matches a standard 36-char UUID.
var uuidRe = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

func init() {
	provider.Register(providerID, func(baseDir string) (domain.SessionProvider, error) {
		return New(baseDir), nil
	})
}

// Provider is the Codex session provider.
type Provider struct {
	dir         string // base ~/.codex directory
	sessionsDir string // ~/.codex/sessions
}

// New returns a Codex provider. If baseDir is empty, $CODEX_HOME or
// ~/.codex is used.
func New(baseDir string) *Provider {
	if baseDir == "" {
		if env := os.Getenv("CODEX_HOME"); env != "" {
			baseDir = env
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				home = "."
			}
			baseDir = filepath.Join(home, ".codex")
		}
	}
	return &Provider{
		dir:         baseDir,
		sessionsDir: filepath.Join(baseDir, "sessions"),
	}
}

// ID returns the provider identifier.
func (p *Provider) ID() string { return providerID }

// Capabilities reports Codex's feature set.
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

// List scans ~/.codex/sessions/ recursively for *.jsonl session files.
func (p *Provider) List(ctx context.Context) ([]domain.Session, error) {
	if _, err := os.Stat(p.sessionsDir); os.IsNotExist(err) {
		return nil, nil
	}

	var sessions []domain.Session
	err := filepath.WalkDir(p.sessionsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}

		id := extractSessionID(d.Name())
		if id == "" {
			return nil
		}

		fi, err := d.Info()
		if err != nil {
			return nil
		}

		cwd, updated := p.inspectSession(path, fi.ModTime().UTC())
		sessions = append(sessions, domain.Session{
			ID:         id,
			ProviderID: providerID,
			CWD:        cwd,
			UpdatedAt:  updated,
			Locked:     false,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})
	return sessions, nil
}

// extractSessionID extracts UUID or filename stem as session ID.
func extractSessionID(filename string) string {
	name := strings.TrimSuffix(filename, ".jsonl")
	name = strings.TrimPrefix(name, "rollout-")
	if loc := uuidRe.FindString(name); loc != "" {
		return loc
	}
	// Fall back to clean token
	parts := strings.Split(name, ".")
	if len(parts) > 1 {
		return parts[len(parts)-1]
	}
	return name
}

// inspectSession checks the first few lines of the file for metadata/cwd.
func (p *Provider) inspectSession(filePath string, defaultModTime time.Time) (cwd string, updated time.Time) {
	updated = defaultModTime
	f, err := os.Open(filePath)
	if err != nil {
		return "", updated
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 2*1024*1024)

	linesChecked := 0
	for scanner.Scan() && linesChecked < 10 {
		linesChecked++
		line := scanner.Bytes()
		var meta struct {
			Type      string `json:"type"`
			CWD       string `json:"cwd"`
			WorkingDir string `json:"working_dir"`
			Timestamp string `json:"timestamp"`
		}
		if err := json.Unmarshal(line, &meta); err == nil {
			if meta.CWD != "" {
				cwd = meta.CWD
			} else if meta.WorkingDir != "" {
				cwd = meta.WorkingDir
			}
			if meta.Timestamp != "" {
				if t, err := time.Parse(time.RFC3339, meta.Timestamp); err == nil {
					updated = t.UTC()
				}
			}
		}
	}
	return cwd, updated
}

// UserPrompts extracts up to max user prompts from the session file.
func (p *Provider) UserPrompts(ctx context.Context, id string, max int) ([]string, error) {
	filePath, err := p.findSessionFile(id)
	if err != nil || filePath == "" {
		return nil, err
	}

	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var prompts []string
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return prompts, ctx.Err()
		}
		if max > 0 && len(prompts) >= max {
			break
		}

		line := scanner.Bytes()
		if !strings.Contains(string(line), `"user"`) {
			continue
		}

		var entry struct {
			Role    string          `json:"role"`
			Type    string          `json:"type"`
			Content json.RawMessage `json:"content"`
			Message struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}

		var rawContent json.RawMessage
		if entry.Role == "user" || entry.Type == "user" {
			if len(entry.Content) > 0 {
				rawContent = entry.Content
			} else if len(entry.Message.Content) > 0 {
				rawContent = entry.Message.Content
			}
		} else if entry.Message.Role == "user" {
			rawContent = entry.Message.Content
		}

		if text := extractText(rawContent); text != "" {
			prompts = append(prompts, text)
		}
	}
	return prompts, scanner.Err()
}

func extractText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}

	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if t := strings.TrimSpace(b.Text); t != "" {
				parts = append(parts, t)
			}
		}
		return strings.Join(parts, " ")
	}
	return ""
}

// findSessionFile locates the .jsonl file for a given session ID.
func (p *Provider) findSessionFile(id string) (string, error) {
	var found string
	err := filepath.WalkDir(p.sessionsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		if extractSessionID(d.Name()) == id {
			found = path
			return fs.SkipAll
		}
		return nil
	})
	return found, err
}

// ValidateResume checks if the codex executable is available on PATH.
func (p *Provider) ValidateResume(ctx context.Context, id string) error {
	_, err := exec.LookPath("codex")
	if err != nil {
		return fmt.Errorf("codex (Codex CLI) not found on PATH: %w\n\nInstall with: npm install -g @openai/codex\nOr ensure 'codex' is in your PATH.", err)
	}
	return nil
}

// Resume replaces current process with `codex resume <id>`.
func (p *Provider) Resume(ctx context.Context, id string) error {
	bin, err := exec.LookPath("codex")
	if err != nil {
		return fmt.Errorf("codex (Codex CLI) not found on PATH: %w\n\nInstall with: npm install -g @openai/codex\nOr ensure 'codex' is in your PATH.", err)
	}
	return syscall.Exec(bin, []string{"codex", "resume", id}, os.Environ())
}

// Children returns nil — Codex has no parent/child links.
func (p *Provider) Children(ctx context.Context, id string) ([]string, error) {
	return nil, nil
}

// Delete removes session rollout files and prunes empty date directories.
func (p *Provider) Delete(ctx context.Context, ids []string) ([]string, error) {
	var deleted []string
	for _, id := range ids {
		path, err := p.findSessionFile(id)
		if err == nil && path != "" {
			if err := os.Remove(path); err == nil {
				deleted = append(deleted, id)
				p.pruneEmptyParentDirs(filepath.Dir(path))
			}
		}
	}
	sort.Strings(deleted)
	return deleted, nil
}

// pruneEmptyParentDirs ascends directory tree up to p.sessionsDir, removing empty dirs.
func (p *Provider) pruneEmptyParentDirs(dir string) {
	cleanBase := filepath.Clean(p.sessionsDir)
	for {
		cleanDir := filepath.Clean(dir)
		if cleanDir == cleanBase || !strings.HasPrefix(cleanDir, cleanBase+string(filepath.Separator)) {
			break
		}
		// os.Remove on a non-empty directory returns an error, which safely leaves it alone.
		if err := os.Remove(cleanDir); err != nil {
			break
		}
		dir = filepath.Dir(cleanDir)
	}
}
