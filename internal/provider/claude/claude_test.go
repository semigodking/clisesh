package claude

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

const (
	testID1 = "a3eb6d25-788a-4979-9f44-b66676d1c6ce"
	testID2 = "b4fc7e36-899b-4a80-af55-c77787e2d7df"
)

func writeClaudeFixture(t *testing.T, baseDir, projName, id, transcript string) {
	t.Helper()
	projDir := filepath.Join(baseDir, "projects", projName)
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.Join(projDir, id+".jsonl")
	if err := os.WriteFile(filePath, []byte(transcript), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setupFixture(t *testing.T) (*Provider, string) {
	t.Helper()
	baseDir := t.TempDir()
	convo1 := `{"type":"permission-mode","permissionMode":"default","sessionId":"a3eb6d25-788a-4979-9f44-b66676d1c6ce"}` + "\n" +
		`{"parentUuid":null,"type":"user","message":{"role":"user","content":"uninstall redsocks"},"cwd":"/home/ubuntu/repo/redsocks","sessionId":"a3eb6d25-788a-4979-9f44-b66676d1c6ce"}` + "\n" +
		`{"parentUuid":null,"type":"user","message":{"role":"user","content":[{"type":"text","text":"second question"}]},"sessionId":"a3eb6d25-788a-4979-9f44-b66676d1c6ce"}` + "\n"

	convo2 := `{"type":"user","message":{"role":"user","content":"analyze git diff"},"sessionId":"b4fc7e36-899b-4a80-af55-c77787e2d7df"}` + "\n"

	writeClaudeFixture(t, baseDir, "-home-ubuntu-repo-redsocks", testID1, convo1)
	writeClaudeFixture(t, baseDir, "-home-ubuntu-workspace", testID2, convo2)

	// Mock active lock for testID1
	sessionsDir := filepath.Join(baseDir, "sessions")
	os.MkdirAll(sessionsDir, 0o755)
	os.WriteFile(filepath.Join(sessionsDir, testID1+".lock"), []byte("active"), 0o644)

	return New(baseDir), baseDir
}

func TestList(t *testing.T) {
	p, _ := setupFixture(t)
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
		if s.ProviderID != "claude" {
			t.Errorf("expected ProviderID claude, got %q", s.ProviderID)
		}
		if s.ID == testID1 {
			if s.CWD != "/home/ubuntu/repo/redsocks" {
				t.Errorf("expected CWD /home/ubuntu/repo/redsocks, got %q", s.CWD)
			}
			if !s.Locked {
				t.Errorf("expected session %s to be locked", testID1)
			}
		}
		if s.ID == testID2 {
			if s.Locked {
				t.Errorf("expected session %s to not be locked", testID2)
			}
			if s.CWD != "/home/ubuntu/workspace" {
				t.Errorf("expected decoded CWD /home/ubuntu/workspace, got %q", s.CWD)
			}
		}
	}
	if !foundMap[testID1] || !foundMap[testID2] {
		t.Errorf("missing expected session ids in list: %+v", foundMap)
	}
}

func TestListPIDSession(t *testing.T) {
	baseDir := t.TempDir()
	convo := `{"type":"user","message":{"role":"user","content":"test pid session"},"sessionId":"` + testID1 + `"}` + "\n"
	writeClaudeFixture(t, baseDir, "-home-ubuntu-proj", testID1, convo)

	sessionsDir := filepath.Join(baseDir, "sessions")
	os.MkdirAll(sessionsDir, 0o755)

	// Create a session file with current process PID (which is alive)
	myPID := os.Getpid()
	aliveData := `{"pid":` + strconv.Itoa(myPID) + `,"sessionId":"` + testID1 + `"}`
	os.WriteFile(filepath.Join(sessionsDir, "alive.json"), []byte(aliveData), 0o644)

	p := New(baseDir)
	sessions, err := p.List(context.Background())
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if !sessions[0].Locked {
		t.Errorf("expected session with alive PID to be locked")
	}

	// Now overwrite with a PID that does not exist (e.g. 9999999)
	deadData := `{"pid":9999999,"sessionId":"` + testID1 + `"}`
	os.WriteFile(filepath.Join(sessionsDir, "alive.json"), []byte(deadData), 0o644)

	sessions, _ = p.List(context.Background())
	if sessions[0].Locked {
		t.Errorf("expected session with dead PID to NOT be locked")
	}
}

func TestUserPrompts(t *testing.T) {
	p, _ := setupFixture(t)
	prompts, err := p.UserPrompts(context.Background(), testID1, 5)
	if err != nil {
		t.Fatalf("UserPrompts error: %v", err)
	}
	if len(prompts) != 2 {
		t.Fatalf("expected 2 prompts, got %d: %v", len(prompts), prompts)
	}
	if prompts[0] != "uninstall redsocks" {
		t.Errorf("prompt[0] = %q, want 'uninstall redsocks'", prompts[0])
	}
	if prompts[1] != "second question" {
		t.Errorf("prompt[1] = %q, want 'second question'", prompts[1])
	}
}

func TestDelete(t *testing.T) {
	p, _ := setupFixture(t)
	deleted, err := p.Delete(context.Background(), []string{testID1})
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	if len(deleted) != 1 || deleted[0] != testID1 {
		t.Fatalf("Delete returned %v, want [%s]", deleted, testID1)
	}

	remaining, _ := p.List(context.Background())
	if len(remaining) != 1 || remaining[0].ID != testID2 {
		t.Fatalf("remaining sessions = %d, want 1 (%s)", len(remaining), testID2)
	}
}

func TestCapabilities(t *testing.T) {
	p := New("")
	caps := p.Capabilities()
	if !caps.CanResume || !caps.CanDelete || !caps.ProvidesPrompts {
		t.Errorf("unexpected capabilities: %+v", caps)
	}
}
