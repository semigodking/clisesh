// Package claude implements domain.SessionProvider for Anthropic's Claude Code CLI.
// Sessions are stored in projects directories: ~/.claude/projects/<project-dir>/<uuid>.jsonl.
// Active sessions/locks are tracked in ~/.claude/sessions/ or process table.
package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"

	"github.com/semigodking/clisesh/internal/domain"
	"github.com/semigodking/clisesh/internal/provider"
)

const providerID = "claude"

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func init() {
	provider.Register(providerID, func(baseDir string) (domain.SessionProvider, error) {
		return New(baseDir), nil
	})
}

// Provider is the Claude Code session provider.
type Provider struct {
	dir         string // base ~/.claude directory
	projectsDir string // ~/.claude/projects
	sessionsDir string // ~/.claude/sessions
}

// New returns a Claude Code provider. If baseDir is empty, $CLAUDE_CONFIG_DIR or
// ~/.claude is used.
func New(baseDir string) *Provider {
	if baseDir == "" {
		if env := os.Getenv("CLAUDE_CONFIG_DIR"); env != "" {
			baseDir = env
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				home = "."
			}
			baseDir = filepath.Join(home, ".claude")
		}
	}
	return &Provider{
		dir:         baseDir,
		projectsDir: filepath.Join(baseDir, "projects"),
		sessionsDir: filepath.Join(baseDir, "sessions"),
	}
}

// ID returns the provider identifier.
func (p *Provider) ID() string { return providerID }

// Capabilities reports Claude Code's feature set.
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

// List scans ~/.claude/projects/ subdirectories for <uuid>.jsonl sessions.
func (p *Provider) List(ctx context.Context) ([]domain.Session, error) {
	projEntries, err := os.ReadDir(p.projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var sessions []domain.Session
	for _, pe := range projEntries {
		if !pe.IsDir() {
			continue
		}
		projPath := filepath.Join(p.projectsDir, pe.Name())
		files, err := os.ReadDir(projPath)
		if err != nil {
			continue
		}

		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".jsonl") {
				continue
			}
			id := strings.TrimSuffix(f.Name(), ".jsonl")
			if !uuidRe.MatchString(id) {
				continue
			}

			fullPath := filepath.Join(projPath, f.Name())
			fi, err := f.Info()
			if err != nil {
				continue
			}

			cwd := p.extractCWD(fullPath, pe.Name())
			sessions = append(sessions, domain.Session{
				ID:         id,
				ProviderID: providerID,
				CWD:        cwd,
				UpdatedAt:  fi.ModTime().UTC(),
				Locked:     p.isLocked(id),
			})
		}
	}

	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})
	return sessions, nil
}

// extractCWD attempts to read the cwd field from early records in the session jsonl,
// falling back to decoding the project directory name (e.g. "-home-ubuntu-repo").
func (p *Provider) extractCWD(filePath, projDirName string) string {
	f, err := os.Open(filePath)
	if err == nil {
		defer f.Close()
		scanner := bufio.NewScanner(f)
		buf := make([]byte, 64*1024)
		scanner.Buffer(buf, 2*1024*1024)

		linesChecked := 0
		for scanner.Scan() && linesChecked < 20 {
			linesChecked++
			line := scanner.Bytes()
			if !strings.Contains(string(line), `"cwd"`) {
				continue
			}
			var entry struct {
				CWD string `json:"cwd"`
			}
			if err := json.Unmarshal(line, &entry); err == nil && entry.CWD != "" {
				return entry.CWD
			}
		}
	}

	// Fallback: decode dash-prefixed folder name
	if strings.HasPrefix(projDirName, "-") {
		return strings.ReplaceAll(projDirName, "-", "/")
	}
	return ""
}

// isLocked checks whether the session is currently active.
// Checks both <id>.lock (used in mock/legacy setups) and <pid>.json in sessionsDir
// with a check verifying the process is currently alive.
func (p *Provider) isLocked(id string) bool {
	lockFile := filepath.Join(p.sessionsDir, id+".lock")
	if fi, err := os.Stat(lockFile); err == nil && !fi.IsDir() {
		return true
	}

	// Check ~/.claude/sessions/<pid>.json files
	entries, err := os.ReadDir(p.sessionsDir)
	if err != nil {
		return false
	}

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(p.sessionsDir, e.Name()))
		if err != nil {
			continue
		}
		var meta struct {
			PID       int    `json:"pid"`
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal(data, &meta); err == nil && meta.SessionID == id {
			if isProcessAlive(meta.PID) {
				return true
			}
		}
	}

	return false
}

// isProcessAlive returns true if the process with the given PID is running.
func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, sending signal 0 checks if process exists without sending an actual signal.
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

// UserPrompts extracts up to max user prompts from the session jsonl.
func (p *Provider) UserPrompts(ctx context.Context, id string, max int) ([]string, error) {
	if !uuidRe.MatchString(id) {
		return nil, nil
	}

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
			Type    string `json:"type"`
			Message struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if entry.Type != "user" && entry.Message.Role != "user" {
			continue
		}

		text := extractTextContent(entry.Message.Content)
		if text != "" {
			prompts = append(prompts, text)
		}
	}
	return prompts, scanner.Err()
}

func extractTextContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Try string
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}

	// Try array of content blocks [{type:"text", text:"..."}]
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

// findSessionFile locates the <id>.jsonl file inside projects/*/.
func (p *Provider) findSessionFile(id string) (string, error) {
	projEntries, err := os.ReadDir(p.projectsDir)
	if err != nil {
		return "", err
	}
	for _, pe := range projEntries {
		if !pe.IsDir() {
			continue
		}
		candidate := filepath.Join(p.projectsDir, pe.Name(), id+".jsonl")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", nil
}

// ValidateResume checks if the claude executable is available on PATH.
func (p *Provider) ValidateResume(ctx context.Context, id string) error {
	if !uuidRe.MatchString(id) {
		return domain.ErrNotSupported
	}
	_, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude (Claude Code CLI) not found on PATH: %w\n\nInstall with: npm install -g @anthropic-ai/claude-code\nOr ensure 'claude' is in your PATH.", err)
	}
	return nil
}

// Resume replaces current process with `claude --resume <id>`.
func (p *Provider) Resume(ctx context.Context, id string) error {
	if !uuidRe.MatchString(id) {
		return domain.ErrNotSupported
	}
	bin, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude (Claude Code CLI) not found on PATH: %w\n\nInstall with: npm install -g @anthropic-ai/claude-code\nOr ensure 'claude' is in your PATH.", err)
	}
	return syscall.Exec(bin, []string{"claude", "--resume", id}, os.Environ())
}

// Children returns nil — Claude Code subagent tracking is internal to transcript.
func (p *Provider) Children(ctx context.Context, id string) ([]string, error) {
	return nil, nil
}

// Delete removes session .jsonl files.
func (p *Provider) Delete(ctx context.Context, ids []string) ([]string, error) {
	var deleted []string
	for _, id := range ids {
		if !uuidRe.MatchString(id) {
			continue
		}
		filePath, err := p.findSessionFile(id)
		if err == nil && filePath != "" {
			if err := os.Remove(filePath); err == nil {
				deleted = append(deleted, id)
			}
		}
	}
	sort.Strings(deleted)
	return deleted, nil
}
