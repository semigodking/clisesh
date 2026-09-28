package codex

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

const (
	testCodexID1 = "d5eb8c21-998a-4979-9f44-a66676d1c6aa"
	testCodexID2 = "e6fc9d32-aa9b-4a80-af55-b77787e2d7bb"
)

func writeCodexFixture(t *testing.T, baseDir, relDir, filename, content string) {
	t.Helper()
	dir := filepath.Join(baseDir, "sessions", relDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(dir, filename)
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setupCodexFixture(t *testing.T) (*Provider, string) {
	t.Helper()
	baseDir := t.TempDir()

	content1 := `{"type":"session_meta","cwd":"/home/ubuntu/repo/app","timestamp":"2026-08-01T10:00:00Z"}` + "\n" +
		`{"role":"user","content":"fix unit test panic in handler"}` + "\n" +
		`{"role":"assistant","content":"investigating handler.go"}` + "\n" +
		`{"type":"user","message":{"role":"user","content":"also check config"}}` + "\n"

	content2 := `{"type":"session_meta","working_dir":"/home/ubuntu/other","timestamp":"2026-08-02T12:00:00Z"}` + "\n" +
		`{"role":"user","content":[{"type":"text","text":"refactor database query"}]}` + "\n"

	writeCodexFixture(t, baseDir, "2026/08/01", "rollout-"+testCodexID1+".jsonl", content1)
	writeCodexFixture(t, baseDir, "2026/08/02", "rollout-"+testCodexID2+".jsonl", content2)

	return New(baseDir), baseDir
}

func TestList(t *testing.T) {
	p, _ := setupCodexFixture(t)
	sessions, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("List got %d sessions, want 2", len(sessions))
	}

	foundMap := map[string]bool{}
	for _, s := range sessions {
		foundMap[s.ID] = true
		if s.ProviderID != "codex" {
			t.Errorf("expected ProviderID codex, got %q", s.ProviderID)
		}
		if s.ID == testCodexID1 {
			if s.CWD != "/home/ubuntu/repo/app" {
				t.Errorf("expected CWD /home/ubuntu/repo/app, got %q", s.CWD)
			}
		}
		if s.ID == testCodexID2 {
			if s.CWD != "/home/ubuntu/other" {
				t.Errorf("expected CWD /home/ubuntu/other, got %q", s.CWD)
			}
		}
	}
	if !foundMap[testCodexID1] || !foundMap[testCodexID2] {
		t.Errorf("missing expected session ids in list: %+v", foundMap)
	}
}

func TestUserPrompts(t *testing.T) {
	p, _ := setupCodexFixture(t)
	prompts, err := p.UserPrompts(context.Background(), testCodexID1, 5)
	if err != nil {
		t.Fatalf("UserPrompts error: %v", err)
	}
	if len(prompts) != 2 {
		t.Fatalf("expected 2 prompts, got %d: %v", len(prompts), prompts)
	}
	if prompts[0] != "fix unit test panic in handler" {
		t.Errorf("prompt[0] = %q, want 'fix unit test panic in handler'", prompts[0])
	}
	if prompts[1] != "also check config" {
		t.Errorf("prompt[1] = %q, want 'also check config'", prompts[1])
	}
}

func TestDelete(t *testing.T) {
	p, baseDir := setupCodexFixture(t)
	deleted, err := p.Delete(context.Background(), []string{testCodexID1})
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != testCodexID1 {
		t.Fatalf("Delete returned %v, want [%s]", deleted, testCodexID1)
	}

	remaining, _ := p.List(context.Background())
	if len(remaining) != 1 || remaining[0].ID != testCodexID2 {
		t.Fatalf("remaining sessions = %d, want 1 (%s)", len(remaining), testCodexID2)
	}

	// Verify that empty 2026/08/01 date directory was pruned.
	emptyDir := filepath.Join(baseDir, "sessions", "2026", "08", "01")
	if _, err := os.Stat(emptyDir); !os.IsNotExist(err) {
		t.Errorf("expected empty date dir %s to be pruned, got err: %v", emptyDir, err)
	}
	// Verify that 2026/08 still exists because 2026/08/02 is there.
	monthDir := filepath.Join(baseDir, "sessions", "2026", "08")
	if _, err := os.Stat(monthDir); err != nil {
		t.Errorf("expected month dir %s to still exist: %v", monthDir, err)
	}
}

func TestCapabilities(t *testing.T) {
	p := New("")
	caps := p.Capabilities()
	if !caps.CanResume || !caps.CanDelete || !caps.ProvidesPrompts {
		t.Errorf("unexpected capabilities: %+v", caps)
	}
}
