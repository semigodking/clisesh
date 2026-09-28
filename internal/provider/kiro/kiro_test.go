package kiro

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeSession creates fixture files for a session in dir.
func writeSession(t *testing.T, dir, id, meta, jsonl string, locked bool) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, id+".json"), []byte(meta), 0o644); err != nil {
		t.Fatal(err)
	}
	if jsonl != "" {
		if err := os.WriteFile(filepath.Join(dir, id+".jsonl"), []byte(jsonl), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if locked {
		os.WriteFile(filepath.Join(dir, id+".lock"), []byte("x"), 0o644)
	}
	os.WriteFile(filepath.Join(dir, id+".history"), []byte("h"), 0o644)
}

const (
	idA = "450ee908-d016-4c2a-ae10-5404d39091b4"
	idB = "1ff13b30-4ec4-4224-9ec6-f825e06ecb44"
	idC = "0799f3c1-9c6d-42b5-819f-f3f5621fde2a" // subagent (child of A)
)

func setup(t *testing.T) *Provider {
	t.Helper()
	dir := t.TempDir()
	writeSession(t, dir, idA,
		`{"title":"Build overview doc","cwd":"/home/u/proj","updated_at":"2026-01-02T10:00:00Z"}`,
		``, false)
	writeSession(t, dir, idB,
		`{"title":"Fix nginx cache","cwd":"/home/u/k8s","updated_at":"2026-01-03T10:00:00Z"}`,
		``, true)
	// Subagent of A — must be excluded from List, found by Children.
	writeSession(t, dir, idC,
		`{"title":"subtask","cwd":"/home/u/proj","updated_at":"2026-01-02T10:05:00Z","parent_session_id":"`+idA+`"}`,
		``, false)
	// A non-UUID json file that must be ignored.
	os.WriteFile(filepath.Join(dir, "notes.json"), []byte(`{}`), 0o644)
	return New(dir)
}

func TestList(t *testing.T) {
	p := setup(t)
	sessions, err := p.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Only A and B (C is a subagent; notes.json is not a UUID).
	if len(sessions) != 2 {
		t.Fatalf("List returned %d sessions, want 2: %+v", len(sessions), sessions)
	}
	// Newest first: B (Jan 3) before A (Jan 2).
	if sessions[0].ID != idB || sessions[1].ID != idA {
		t.Errorf("order wrong: %s then %s", sessions[0].ID, sessions[1].ID)
	}
	if sessions[0].Title != "Fix nginx cache" {
		t.Errorf("title = %q", sessions[0].Title)
	}
	if !sessions[0].Locked {
		t.Error("B should be locked (.lock present)")
	}
	if sessions[1].Locked {
		t.Error("A should not be locked")
	}
	if sessions[0].ProviderID != "kiro" {
		t.Errorf("ProviderID = %q", sessions[0].ProviderID)
	}
}

func TestUserPromptsSkipsHugeToolResults(t *testing.T) {
	dir := t.TempDir()
	huge := strings.Repeat("x", 200000) // 200KB tool-result line between prompts
	jsonl := `{"kind":"Prompt","data":{"content":[{"kind":"text","data":"first prompt"}]}}` + "\n" +
		`{"kind":"ToolResults","data":{"content":[{"kind":"text","data":"` + huge + `"}]}}` + "\n" +
		`{"kind":"AssistantMessage","data":{"content":[{"kind":"text","data":"reply"}]}}` + "\n" +
		`{"kind":"Prompt","data":{"content":[{"kind":"text","data":"second prompt"}]}}` + "\n"
	writeSession(t, dir, idA, `{"title":"t"}`, jsonl, false)

	p := New(dir)
	prompts, err := p.UserPrompts(context.Background(), idA, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(prompts) != 2 {
		t.Fatalf("expected 2 prompts, got %d: %v", len(prompts), prompts)
	}
	if prompts[0] != "first prompt" || prompts[1] != "second prompt" {
		t.Errorf("unexpected prompts: %v", prompts)
	}
}

func TestUserPromptsRespectsMax(t *testing.T) {
	dir := t.TempDir()
	jsonl := ""
	for i := 0; i < 5; i++ {
		jsonl += `{"kind":"Prompt","data":{"content":[{"kind":"text","data":"p"}]}}` + "\n"
	}
	writeSession(t, dir, idA, `{"title":"t"}`, jsonl, false)
	p := New(dir)
	prompts, _ := p.UserPrompts(context.Background(), idA, 3)
	if len(prompts) != 3 {
		t.Errorf("max not respected: got %d, want 3", len(prompts))
	}
}

func TestChildren(t *testing.T) {
	p := setup(t)
	kids, err := p.Children(context.Background(), idA)
	if err != nil {
		t.Fatal(err)
	}
	if len(kids) != 1 || kids[0] != idC {
		t.Errorf("Children(A) = %v, want [%s]", kids, idC)
	}
}

func TestDeleteCascades(t *testing.T) {
	p := setup(t)
	deleted, err := p.Delete(context.Background(), []string{idA})
	if err != nil {
		t.Fatal(err)
	}
	// A and its child C should both be deleted.
	got := map[string]bool{}
	for _, id := range deleted {
		got[id] = true
	}
	if !got[idA] || !got[idC] {
		t.Errorf("Delete(A) deleted = %v, want to include A and C", deleted)
	}
	// Files for A and C must be gone; B must remain.
	if _, err := os.Stat(filepath.Join(p.dir, idA+".json")); !os.IsNotExist(err) {
		t.Error("A.json should be deleted")
	}
	if _, err := os.Stat(filepath.Join(p.dir, idC+".json")); !os.IsNotExist(err) {
		t.Error("C.json (child) should be deleted")
	}
	if _, err := os.Stat(filepath.Join(p.dir, idB+".json")); err != nil {
		t.Error("B.json should still exist")
	}
}

func TestDeleteRejectsInvalidID(t *testing.T) {
	p := setup(t)
	deleted, err := p.Delete(context.Background(), []string{"../../etc/passwd", "not-a-uuid"})
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 0 {
		t.Errorf("invalid ids must delete nothing, got %v", deleted)
	}
}

func TestCapabilities(t *testing.T) {
	c := New("").Capabilities()
	if !c.HasTitle || !c.HasParent || !c.CanResume || !c.CanDelete || !c.CascadeDelete || !c.ProvidesPrompts {
		t.Errorf("Kiro should have all capabilities: %+v", c)
	}
}

func TestListMissingDir(t *testing.T) {
	p := New(filepath.Join(t.TempDir(), "does-not-exist"))
	sessions, err := p.List(context.Background())
	if err != nil {
		t.Errorf("missing dir should not error: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("missing dir should yield 0 sessions, got %d", len(sessions))
	}
}
