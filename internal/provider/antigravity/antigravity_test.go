package antigravity

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

const (
	idA = "450ee908-d016-4c2a-ae10-5404d39091b4"
	idB = "1ff13b30-4ec4-4224-9ec6-f825e06ecb44"
)

// writeConvo creates a fixture conversation dir with a transcript.
func writeConvo(t *testing.T, brain, id, transcript string) {
	t.Helper()
	logs := filepath.Join(brain, id, ".system_generated", "logs")
	if err := os.MkdirAll(logs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logs, "transcript.jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatal(err)
	}
}

func setup(t *testing.T) (*Provider, string) {
	brain := t.TempDir()
	writeConvo(t, brain, idA,
		`{"type":"USER_INPUT","text":"write integration tests for the webhook"}`+"\n"+
			`{"type":"PLANNER_RESPONSE","text":"looking at the middleware"}`+"\n")
	writeConvo(t, brain, idB,
		`{"type":"USER_INPUT","message":{"text":"why is the ETL job failing"}}`+"\n")
	// Non-UUID dir must be ignored.
	os.MkdirAll(filepath.Join(brain, "not-a-uuid"), 0o755)
	return New(brain), brain
}

func TestList(t *testing.T) {
	p, _ := setup(t)
	sessions, err := p.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("List = %d sessions, want 2", len(sessions))
	}
	for _, s := range sessions {
		if s.ProviderID != "antigravity" {
			t.Errorf("ProviderID = %q", s.ProviderID)
		}
		if s.Title != "" {
			t.Errorf("Antigravity sessions should have no title, got %q", s.Title)
		}
		if s.UpdatedAt.IsZero() {
			t.Error("UpdatedAt should be set from transcript mtime")
		}
	}
}

func TestUserPromptsUserInputOnly(t *testing.T) {
	p, _ := setup(t)
	// idA: top-level text field.
	prompts, err := p.UserPrompts(context.Background(), idA, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 1 || prompts[0] != "write integration tests for the webhook" {
		t.Errorf("idA prompts = %v", prompts)
	}
	// idB: nested message.text field.
	prompts, _ = p.UserPrompts(context.Background(), idB, 10)
	if len(prompts) != 1 || prompts[0] != "why is the ETL job failing" {
		t.Errorf("idB prompts = %v", prompts)
	}
}

func TestCapabilities(t *testing.T) {
	c := New("").Capabilities()
	if c.HasTitle {
		t.Error("HasTitle should be false")
	}
	if c.HasParent {
		t.Error("HasParent should be false")
	}
	if c.CascadeDelete {
		t.Error("CascadeDelete should be false")
	}
	if !c.CanResume || !c.CanDelete || !c.ProvidesPrompts {
		t.Errorf("CanResume/CanDelete/ProvidesPrompts should be true: %+v", c)
	}
}

func TestDeleteRemovesDir(t *testing.T) {
	p, brain := setup(t)
	// Create presence lock file for idA
	lockPath := filepath.Join(p.presenceDir, idA+".lock")
	_ = os.MkdirAll(p.presenceDir, 0o755)
	_ = os.WriteFile(lockPath, []byte(""), 0o644)

	deleted, err := p.Delete(context.Background(), []string{idA})
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 1 || deleted[0] != idA {
		t.Errorf("deleted = %v, want [%s]", deleted, idA)
	}
	if _, err := os.Stat(filepath.Join(brain, idA)); !os.IsNotExist(err) {
		t.Error("conversation dir should be removed")
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Error("presence lock file should also be removed")
	}
	// idB dir must remain.
	if _, err := os.Stat(filepath.Join(brain, idB)); err != nil {
		t.Error("idB dir should still exist")
	}
}

func TestDeleteRejectsUnsafeIDs(t *testing.T) {
	p, brain := setup(t)
	// Put a sentinel file at the brain root to ensure it's never removed.
	sentinel := filepath.Join(brain, "SENTINEL")
	os.WriteFile(sentinel, []byte("x"), 0o644)

	bad := []string{"", ".", "..", "../brain", "a/b", `a\b`, "not-a-uuid"}
	deleted, err := p.Delete(context.Background(), bad)
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 0 {
		t.Errorf("unsafe ids must delete nothing, got %v", deleted)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Error("brain root contents must be untouched")
	}
	if _, err := os.Stat(brain); err != nil {
		t.Error("brain root must still exist")
	}
}

func TestChildrenNil(t *testing.T) {
	p, _ := setup(t)
	kids, err := p.Children(context.Background(), idA)
	if err != nil || kids != nil {
		t.Errorf("Children should be (nil,nil), got (%v,%v)", kids, err)
	}
}

func TestListMissingDir(t *testing.T) {
	p := New(filepath.Join(t.TempDir(), "nope"))
	sessions, err := p.List(context.Background())
	if err != nil {
		t.Errorf("missing dir should not error: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("want 0 sessions, got %d", len(sessions))
	}
}

func TestActiveSessionDetection(t *testing.T) {
	p, brain := setup(t)
	presenceDir := filepath.Join(filepath.Dir(brain), "presence")
	if err := os.MkdirAll(presenceDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// 1. Initially neither session has a lock file -> both Locked == false.
	sessions, err := p.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range sessions {
		if s.Locked {
			t.Errorf("session %s should not be locked initially", s.ID)
		}
	}

	// 2. Create lock file for idA and acquire an exclusive flock (simulating active agy process).
	lockPathA := filepath.Join(presenceDir, idA+".lock")
	lockFileA, err := os.OpenFile(lockPathA, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer lockFileA.Close()

	if err := syscall.Flock(int(lockFileA.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("failed to acquire test flock: %v", err)
	}

	// Also create an unlocked lock file for idB (simulating leftover lock file after process exits).
	lockPathB := filepath.Join(presenceDir, idB+".lock")
	if err := os.WriteFile(lockPathB, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	// 3. List sessions now: idA should be Locked == true, idB should be Locked == false.
	sessions, err = p.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var lockedA, lockedB bool
	for _, s := range sessions {
		if s.ID == idA {
			lockedA = s.Locked
		}
		if s.ID == idB {
			lockedB = s.Locked
		}
	}
	if !lockedA {
		t.Errorf("session idA should be detected as Locked (active), got false")
	}
	if lockedB {
		t.Errorf("session idB should be detected as unlocked, got true")
	}

	// 4. Release lock on idA. List again: idA should now be Locked == false.
	_ = syscall.Flock(int(lockFileA.Fd()), syscall.LOCK_UN)
	sessions, _ = p.List(context.Background())
	for _, s := range sessions {
		if s.ID == idA && s.Locked {
			t.Errorf("session idA should be unlocked after releasing flock, got locked")
		}
	}
}
