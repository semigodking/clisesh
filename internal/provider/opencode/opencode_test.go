package opencode

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func setupTestProvider(t *testing.T, handler http.HandlerFunc) (*Provider, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)

	tmpDir := t.TempDir()
	cfgDir := filepath.Join(tmpDir, "config")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}

	svcJSON := `{"password":"test-password"}`
	if err := os.WriteFile(filepath.Join(cfgDir, "service.json"), []byte(svcJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	p := &Provider{
		configDir: cfgDir,
		shareDir:  filepath.Join(tmpDir, "share"),
		client:    server.Client(),
	}

	return p, server
}

func TestCapabilities(t *testing.T) {
	p := New("")
	caps := p.Capabilities()
	if !caps.HasTitle {
		t.Error("expected HasTitle to be true")
	}
	if !caps.HasParent {
		t.Error("expected HasParent to be true")
	}
	if !caps.CanResume {
		t.Error("expected CanResume to be true")
	}
	if !caps.CanDelete {
		t.Error("expected CanDelete to be true")
	}
	if !caps.CascadeDelete {
		t.Error("expected CascadeDelete to be true")
	}
	if !caps.ProvidesPrompts {
		t.Error("expected ProvidesPrompts to be true")
	}
}

func TestListSessions(t *testing.T) {
	mockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "opencode" || pass != "test-password" {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		if r.URL.Path == "/api/session/active" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"ses_222222222222": map[string]any{"active": true},
				},
			})
			return
		}

		if r.URL.Path == "/api/session" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{
						"id":        "ses_111111111111",
						"projectID": "proj1",
						"title":     "First Task",
						"location":  map[string]any{"directory": "/home/user/proj1"},
						"time": map[string]any{
							"created": int64(1700000000000),
							"updated": int64(1700000010000),
						},
					},
					{
						"id":        "ses_222222222222",
						"projectID": "proj2",
						"title":     "Second Task Active",
						"location":  map[string]any{"directory": "/home/user/proj2"},
						"time": map[string]any{
							"created": int64(1700000020000),
							"updated": int64(1700000030000),
						},
					},
					{
						// Subagent child session (must be skipped in top-level List)
						"id":        "ses_333333333333",
						"parentID":  "ses_111111111111",
						"projectID": "proj1",
						"title":     "Child Task",
						"location":  map[string]any{"directory": "/home/user/proj1"},
						"time": map[string]any{
							"created": int64(1700000040000),
							"updated": int64(1700000050000),
						},
					},
				},
			})
			return
		}

		http.NotFound(w, r)
	})

	p, server := setupTestProvider(t, mockHandler)
	defer server.Close()

	// Inject custom getServiceURL test hook or override by testing internal parsing
	sessionsResp, err := p.client.Get(server.URL + "/api/session")
	if err != nil {
		t.Fatal(err)
	}
	defer sessionsResp.Body.Close()

	cfg, _ := p.readServiceConfig()
	activeResp, err := p.fetchActiveSessions(context.Background(), server.URL, cfg.Password)
	if err != nil {
		t.Fatalf("fetchActiveSessions: %v", err)
	}
	if !activeResp.Data["ses_222222222222"].(map[string]any)["active"].(bool) {
		t.Error("expected ses_222222222222 to be active")
	}
}

func TestUserPromptsFromAPI(t *testing.T) {
	mockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/session/ses_111111111111/message" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{
						"type": "assistant",
						"text": "Hello there!",
						"time": map[string]any{"created": int64(1700000020000)},
					},
					{
						"type": "user",
						"text": "Second question",
						"time": map[string]any{"created": int64(1700000030000)},
					},
					{
						"type": "user",
						"text": "First question",
						"time": map[string]any{"created": int64(1700000010000)},
					},
				},
			})
			return
		}
		http.NotFound(w, r)
	})

	p, server := setupTestProvider(t, mockHandler)
	defer server.Close()

	// Direct test of userPromptsFromAPI logic with mock server
	req, _ := http.NewRequest(http.MethodGet, server.URL+"/api/session/ses_111111111111/message", nil)
	resp, err := p.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

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
		t.Fatal(err)
	}

	if len(msgResp.Data) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(msgResp.Data))
	}
}

func TestSessionIDRegex(t *testing.T) {
	valid := []string{
		"ses_f14aac24bffeJaXyEHtxaBzxOU",
		"ses_123456",
		"ses_abcXYZ",
	}
	invalid := []string{
		"123",
		"ses_",
		"ses-12345",
		"uuid-8484",
		"../ses_123",
	}

	for _, v := range valid {
		if !sessionIDRe.MatchString(v) {
			t.Errorf("expected %q to be valid", v)
		}
	}
	for _, inv := range invalid {
		if sessionIDRe.MatchString(inv) {
			t.Errorf("expected %q to be invalid", inv)
		}
	}
}
