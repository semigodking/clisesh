// Package opencode implements domain.SessionProvider for the OpenCode CLI agent.
// OpenCode manages sessions via a local SQLite database (~/.local/share/opencode/opencode.db)
// and an authenticated local HTTP service (http://127.0.0.1:<port>).
// This provider communicates with the OpenCode service API for fast cross-project
// session discovery, active lock detection, and message transcript extraction,
// with graceful fallback to CLI commands (`opencode session export / delete`).
package opencode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

const providerID = "opencode"

// sessionIDRe validates canonical OpenCode session IDs (e.g. "ses_f14aac24bffeJaXyEHtxaBzxOU").
var sessionIDRe = regexp.MustCompile(`^ses_[0-9a-zA-Z]+$`)

func init() {
	provider.Register(providerID, func(baseDir string) (domain.SessionProvider, error) {
		return New(baseDir), nil
	})
}

// Provider is the OpenCode session provider.
type Provider struct {
	configDir string
	shareDir  string
	client    *http.Client
}

// New returns an OpenCode provider.
func New(baseDir string) *Provider {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	cfgDir := filepath.Join(home, ".config", "opencode")
	shrDir := filepath.Join(home, ".local", "share", "opencode")
	if baseDir != "" {
		cfgDir = filepath.Join(baseDir, "config")
		shrDir = filepath.Join(baseDir, "share")
	}

	return &Provider{
		configDir: cfgDir,
		shareDir:  shrDir,
		client: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// ID returns the provider identifier.
func (p *Provider) ID() string { return providerID }

// Capabilities reports OpenCode's feature set.
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

// serviceConfig holds the auth credentials for OpenCode local service.
type serviceConfig struct {
	Password string `json:"password"`
}

// readServiceConfig loads the service password from ~/.config/opencode/service.json.
func (p *Provider) readServiceConfig() (*serviceConfig, error) {
	path := filepath.Join(p.configDir, "service.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg serviceConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// getServiceURL gets the current running service URL via `opencode service status`.
func (p *Provider) getServiceURL(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "opencode", "service", "status")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	urlStr := strings.TrimSpace(string(out))
	if !strings.HasPrefix(urlStr, "http://") && !strings.HasPrefix(urlStr, "https://") {
		return "", fmt.Errorf("opencode service not running: %s", urlStr)
	}
	return urlStr, nil
}

// apiSessionResponse represents the JSON returned by GET /api/session.
type apiSessionResponse struct {
	Data []struct {
		ID        string `json:"id"`
		ProjectID string `json:"projectID"`
		ParentID  string `json:"parentID,omitempty"`
		Title     string `json:"title"`
		Location  struct {
			Directory string `json:"directory"`
		} `json:"location"`
		Time struct {
			Created int64 `json:"created"`
			Updated int64 `json:"updated"`
			Idle    int64 `json:"idle"`
		} `json:"time"`
	} `json:"data"`
}

// apiActiveSessionResponse represents the JSON returned by GET /api/session/active.
type apiActiveSessionResponse struct {
	Data map[string]any `json:"data"`
}

// List returns all top-level OpenCode sessions, newest first.
func (p *Provider) List(ctx context.Context) ([]domain.Session, error) {
	serviceURL, err := p.getServiceURL(ctx)
	if err == nil {
		if cfg, err := p.readServiceConfig(); err == nil {
			if sessions, err := p.listFromAPI(ctx, serviceURL, cfg.Password); err == nil && len(sessions) > 0 {
				return sessions, nil
			}
		}
	}

	// Fallback 1: Query global opencode.db directly (works from any directory, service running or stopped)
	if sessions, err := p.listFromDB(ctx); err == nil && len(sessions) > 0 {
		return sessions, nil
	}

	// Fallback 2: Local CLI list (scoped to current working directory)
	return p.listFromCLI(ctx)
}

func (p *Provider) listFromAPI(ctx context.Context, serviceURL, password string) ([]domain.Session, error) {
	// 1. Fetch active sessions map for lock checking
	activeMap := map[string]bool{}
	if activeResp, err := p.fetchActiveSessions(ctx, serviceURL, password); err == nil {
		for id := range activeResp.Data {
			activeMap[id] = true
		}
	}

	// 2. Fetch all global sessions
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serviceURL+"/api/session", nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth("opencode", password)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("session api returned status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var sessionResp apiSessionResponse
	if err := json.Unmarshal(body, &sessionResp); err != nil {
		return nil, fmt.Errorf("parse opencode sessions json: %w", err)
	}

	var out []domain.Session
	for _, it := range sessionResp.Data {
		if !sessionIDRe.MatchString(it.ID) {
			continue
		}
		// Subagent / child sessions are omitted from the top-level list
		if it.ParentID != "" {
			continue
		}

		updatedAt := time.UnixMilli(it.Time.Updated)
		if it.Time.Updated == 0 && it.Time.Created > 0 {
			updatedAt = time.UnixMilli(it.Time.Created)
		}

		out = append(out, domain.Session{
			ID:         it.ID,
			ProviderID: providerID,
			Title:      it.Title,
			CWD:        it.Location.Directory,
			UpdatedAt:  updatedAt,
			Locked:     activeMap[it.ID],
			ParentID:   it.ParentID,
		})
	}

	sort.Slice(out, func(i, j int) bool {
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})

	return out, nil
}

// dbSessionItem models session rows queried from opencode.db.
type dbSessionItem struct {
	ID        string `json:"id"`
	ParentID  string `json:"parent_id"`
	Directory string `json:"directory"`
	Title     string `json:"title"`
	Created   int64  `json:"created"`
	Updated   int64  `json:"updated"`
}

// listFromDB queries the SQLite database directly across all projects.
func (p *Provider) listFromDB(ctx context.Context) ([]domain.Session, error) {
	dbPath := filepath.Join(p.shareDir, "opencode.db")
	if _, err := os.Stat(dbPath); err != nil {
		return nil, err
	}

	pyScript := `
import sqlite3, os, sys, json

db = sys.argv[1]
if not os.path.exists(db):
    print(json.dumps([]))
    sys.exit(0)

try:
    conn = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
    cur = conn.cursor()
    cur.execute("SELECT name FROM sqlite_master WHERE type='table' AND name IN ('session_v2', 'session')")
    tables = [r[0] for r in cur.fetchall()]
    table = 'session_v2' if 'session_v2' in tables else ('session' if 'session' in tables else None)
    if not table:
        print(json.dumps([]))
        sys.exit(0)

    cur.execute(f"PRAGMA table_info({table})")
    cols = {r[1] for r in cur.fetchall()}
    archived_clause = "WHERE (time_archived IS NULL OR time_archived = 0)" if 'time_archived' in cols else ""

    query = f"""
        SELECT id, COALESCE(parent_id, ''), COALESCE(directory, ''), COALESCE(title, ''), time_created, time_updated
        FROM {table}
        {archived_clause}
        ORDER BY time_updated DESC
    """
    cur.execute(query)
    out = []
    for r in cur.fetchall():
        out.append({
            'id': r[0],
            'parent_id': r[1],
            'directory': r[2],
            'title': r[3],
            'created': r[4],
            'updated': r[5],
        })
    print(json.dumps(out))
except Exception:
    print(json.dumps([]))
`
	cmd := exec.CommandContext(ctx, "python3", "-c", pyScript, dbPath)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var items []dbSessionItem
	if err := json.Unmarshal(out, &items); err != nil {
		return nil, err
	}

	var sessions []domain.Session
	for _, it := range items {
		if !sessionIDRe.MatchString(it.ID) {
			continue
		}
		// Subagent child sessions are skipped in top-level List
		if it.ParentID != "" {
			continue
		}
		updatedAt := time.UnixMilli(it.Updated)
		if it.Updated == 0 && it.Created > 0 {
			updatedAt = time.UnixMilli(it.Created)
		}
		sessions = append(sessions, domain.Session{
			ID:         it.ID,
			ProviderID: providerID,
			Title:      it.Title,
			CWD:        it.Directory,
			UpdatedAt:  updatedAt,
			ParentID:   it.ParentID,
		})
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	return sessions, nil
}

func (p *Provider) fetchActiveSessions(ctx context.Context, serviceURL, password string) (*apiActiveSessionResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serviceURL+"/api/session/active", nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth("opencode", password)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("active session status: %d", resp.StatusCode)
	}
	var activeResp apiActiveSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&activeResp); err != nil {
		return nil, err
	}
	return &activeResp, nil
}

// listFromCLI uses `opencode session list --format json` as fallback.
func (p *Provider) listFromCLI(ctx context.Context) ([]domain.Session, error) {
	cmd := exec.CommandContext(ctx, "opencode", "session", "list", "--format", "json")
	out, err := cmd.Output()
	if err != nil {
		return nil, nil // If opencode is not installed or errors, gracefully return empty list
	}

	var items []struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Updated   int64  `json:"updated"`
		Created   int64  `json:"created"`
		Directory string `json:"directory"`
	}
	if err := json.Unmarshal(out, &items); err != nil {
		return nil, nil
	}

	var sessions []domain.Session
	for _, it := range items {
		if !sessionIDRe.MatchString(it.ID) {
			continue
		}
		updatedAt := time.UnixMilli(it.Updated)
		if it.Updated == 0 && it.Created > 0 {
			updatedAt = time.UnixMilli(it.Created)
		}
		sessions = append(sessions, domain.Session{
			ID:         it.ID,
			ProviderID: providerID,
			Title:      it.Title,
			CWD:        it.Directory,
			UpdatedAt:  updatedAt,
		})
	}
	return sessions, nil
}

// Children returns the IDs of all subagent sessions spawned by the session with the given parentID.
func (p *Provider) Children(ctx context.Context, parentID string) ([]string, error) {
	if !sessionIDRe.MatchString(parentID) {
		return nil, nil
	}

	serviceURL, err := p.getServiceURL(ctx)
	if err == nil {
		if cfg, err := p.readServiceConfig(); err == nil {
			if childIDs, err := p.childrenFromAPI(ctx, serviceURL, cfg.Password, parentID); err == nil && len(childIDs) > 0 {
				return childIDs, nil
			}
		}
	}

	// Fallback to SQLite DB
	return p.childrenFromDB(ctx, parentID)
}

func (p *Provider) childrenFromAPI(ctx context.Context, serviceURL, password, parentID string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, serviceURL+"/api/session", nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth("opencode", password)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status: %d", resp.StatusCode)
	}

	var sessionResp apiSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&sessionResp); err != nil {
		return nil, err
	}

	var childIDs []string
	for _, it := range sessionResp.Data {
		if it.ParentID == parentID && sessionIDRe.MatchString(it.ID) {
			childIDs = append(childIDs, it.ID)
		}
	}
	return childIDs, nil
}

func (p *Provider) childrenFromDB(ctx context.Context, parentID string) ([]string, error) {
	dbPath := filepath.Join(p.shareDir, "opencode.db")
	if _, err := os.Stat(dbPath); err != nil {
		return nil, nil
	}

	pyScript := `
import sqlite3, os, sys, json

db, parent_id = sys.argv[1], sys.argv[2]
if not os.path.exists(db):
    print(json.dumps([]))
    sys.exit(0)

try:
    conn = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
    cur = conn.cursor()
    cur.execute("SELECT name FROM sqlite_master WHERE type='table' AND name IN ('session_v2', 'session')")
    tables = [r[0] for r in cur.fetchall()]
    table = 'session_v2' if 'session_v2' in tables else ('session' if 'session' in tables else None)
    if not table:
        print(json.dumps([]))
        sys.exit(0)

    cur.execute(f"SELECT id FROM {table} WHERE parent_id = ?", (parent_id,))
    out = [r[0] for r in cur.fetchall()]
    print(json.dumps(out))
except Exception:
    print(json.dumps([]))
`
	cmd := exec.CommandContext(ctx, "python3", "-c", pyScript, dbPath, parentID)
	out, err := cmd.Output()
	if err != nil {
		return nil, nil
	}
	var childIDs []string
	_ = json.Unmarshal(out, &childIDs)
	return childIDs, nil
}

// UserPrompts extracts up to max user prompts from the session messages.
func (p *Provider) UserPrompts(ctx context.Context, id string, max int) ([]string, error) {
	if !sessionIDRe.MatchString(id) {
		return nil, nil
	}

	// 1. Try service API first
	if prompts, err := p.userPromptsFromAPI(ctx, id, max); err == nil && len(prompts) > 0 {
		return prompts, nil
	}

	// 2. Try direct SQLite DB read across all projects
	if prompts, err := p.userPromptsFromDB(ctx, id, max); err == nil && len(prompts) > 0 {
		return prompts, nil
	}

	// 3. Fallback to CLI export
	return p.userPromptsFromCLI(ctx, id, max)
}

func (p *Provider) userPromptsFromDB(ctx context.Context, id string, max int) ([]string, error) {
	dbPath := filepath.Join(p.shareDir, "opencode.db")
	if _, err := os.Stat(dbPath); err != nil {
		return nil, err
	}

	pyScript := `
import sqlite3, os, sys, json

db, session_id = sys.argv[1], sys.argv[2]
if not os.path.exists(db):
    print(json.dumps([]))
    sys.exit(0)

try:
    conn = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
    cur = conn.cursor()
    cur.execute("SELECT name FROM sqlite_master WHERE type='table' AND name = 'session_message'")
    if not cur.fetchone():
        print(json.dumps([]))
        sys.exit(0)

    cur.execute("""
        SELECT data FROM session_message
        WHERE session_id = ? AND type = 'user'
        ORDER BY seq ASC, time_created ASC
    """, (session_id,))
    prompts = []
    for r in cur.fetchall():
        try:
            d = json.loads(r[0])
            txt = d.get('text', '').strip()
            if txt:
                prompts.append(txt)
        except Exception:
            pass
    print(json.dumps(prompts))
except Exception:
    print(json.dumps([]))
`
	cmd := exec.CommandContext(ctx, "python3", "-c", pyScript, dbPath, id)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var allPrompts []string
	if err := json.Unmarshal(out, &allPrompts); err != nil {
		return nil, err
	}
	if max > 0 && len(allPrompts) > max {
		return allPrompts[:max], nil
	}
	return allPrompts, nil
}

func (p *Provider) userPromptsFromAPI(ctx context.Context, id string, max int) ([]string, error) {
	serviceURL, err := p.getServiceURL(ctx)
	if err != nil {
		return nil, err
	}
	cfg, err := p.readServiceConfig()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/api/session/%s/message", serviceURL, id), nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth("opencode", cfg.Password)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch messages status: %d", resp.StatusCode)
	}

	var msgResp struct {
		Data []struct {
			Type string `json:"type"`
			Text string `json:"text"`
			Time struct {
				Created int64 `json:"created"`
			} `json:"time"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&msgResp); err != nil {
		return nil, err
	}

	// Sort messages chronologically by creation time (API returns in reverse cursor order)
	type userMsg struct {
		time int64
		text string
	}
	var userMsgs []userMsg
	for _, m := range msgResp.Data {
		if m.Type == "user" && strings.TrimSpace(m.Text) != "" {
			userMsgs = append(userMsgs, userMsg{time: m.Time.Created, text: strings.TrimSpace(m.Text)})
		}
	}
	sort.Slice(userMsgs, func(i, j int) bool {
		return userMsgs[i].time < userMsgs[j].time
	})

	var prompts []string
	for _, m := range userMsgs {
		prompts = append(prompts, m.text)
		if max > 0 && len(prompts) >= max {
			break
		}
	}
	return prompts, nil
}

func (p *Provider) userPromptsFromCLI(ctx context.Context, id string, max int) ([]string, error) {
	cmd := exec.CommandContext(ctx, "opencode", "session", "export", id)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var exported struct {
		Messages []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(out, &exported); err != nil {
		return nil, err
	}

	var prompts []string
	for _, m := range exported.Messages {
		if m.Type == "user" && strings.TrimSpace(m.Text) != "" {
			prompts = append(prompts, strings.TrimSpace(m.Text))
			if max > 0 && len(prompts) >= max {
				break
			}
		}
	}
	return prompts, nil
}

// ValidateResume checks if the opencode binary is available in PATH.
func (p *Provider) ValidateResume(ctx context.Context, id string) error {
	if !sessionIDRe.MatchString(id) {
		return domain.ErrNotSupported
	}
	_, err := exec.LookPath("opencode")
	if err != nil {
		return fmt.Errorf("opencode CLI not found on PATH: %w\n\nInstall with: npm install -g @opencode/cli\nOr ensure 'opencode' is in your PATH.", err)
	}
	return nil
}

// Resume replaces the current process with `opencode --session <id>`.
func (p *Provider) Resume(ctx context.Context, id string) error {
	if !sessionIDRe.MatchString(id) {
		return domain.ErrNotSupported
	}
	bin, err := exec.LookPath("opencode")
	if err != nil {
		return fmt.Errorf("opencode CLI not found on PATH: %w", err)
	}

	args := []string{"opencode", "--session", id}
	return syscall.Exec(bin, args, os.Environ())
}

// Delete permanently removes the sessions with the given IDs using `opencode session delete`.
func (p *Provider) Delete(ctx context.Context, ids []string) ([]string, error) {
	var deleted []string
	var errs []string

	for _, id := range ids {
		if !sessionIDRe.MatchString(id) {
			continue
		}
		cmd := exec.CommandContext(ctx, "opencode", "session", "delete", id)
		if out, err := cmd.CombinedOutput(); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v (%s)", id, err, strings.TrimSpace(string(out))))
		} else {
			deleted = append(deleted, id)
		}
	}

	if len(errs) > 0 {
		return deleted, errors.New(strings.Join(errs, "; "))
	}
	return deleted, nil
}
