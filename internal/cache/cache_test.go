package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := Open(dir, "kiro")
	c.Set("id1", "first description")
	c.Set("id2", "second description")
	if err := c.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	c2 := Open(dir, "kiro")
	if got, ok := c2.Get("id1"); !ok || got != "first description" {
		t.Errorf("Get(id1) = %q,%v", got, ok)
	}
	if c2.Len() != 2 {
		t.Errorf("Len = %d, want 2", c2.Len())
	}
}

func TestPerProviderIsolation(t *testing.T) {
	dir := t.TempDir()
	k := Open(dir, "kiro")
	k.Set("x", "kiro-desc")
	k.Save()

	a := Open(dir, "antigravity")
	if _, ok := a.Get("x"); ok {
		t.Error("antigravity cache should not see kiro entries")
	}
	if a.Path() == k.Path() {
		t.Error("providers must use distinct cache files")
	}
}

func TestVersionInvalidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kiro-descriptions.json")
	// Write a cache file with a wrong version.
	os.WriteFile(path, []byte(`{"__version__": 999, "id1": "stale"}`), 0o644)

	c := Open(dir, "kiro")
	if c.Len() != 0 {
		t.Errorf("stale-version cache should load empty, got Len=%d", c.Len())
	}
	if _, ok := c.Get("id1"); ok {
		t.Error("stale entry should not be loaded")
	}
}

func TestCorruptFileLoadsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kiro-descriptions.json")
	os.WriteFile(path, []byte("{not valid json"), 0o644)
	c := Open(dir, "kiro")
	if c.Len() != 0 {
		t.Errorf("corrupt cache should load empty, got Len=%d", c.Len())
	}
}

func TestDelete(t *testing.T) {
	dir := t.TempDir()
	c := Open(dir, "kiro")
	c.Set("a", "A")
	c.Set("b", "B")
	c.Delete("a")
	if _, ok := c.Get("a"); ok {
		t.Error("deleted entry still present")
	}
	if c.Len() != 1 {
		t.Errorf("Len = %d, want 1", c.Len())
	}
}

func TestAtomicSaveNoTempLeftover(t *testing.T) {
	dir := t.TempDir()
	c := Open(dir, "kiro")
	c.Set("a", "A")
	if err := c.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// The saved file must be valid JSON with the version key.
	data, _ := os.ReadFile(c.Path())
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("saved file not valid JSON: %v", err)
	}
	if _, ok := m["__version__"]; !ok {
		t.Error("saved file missing __version__")
	}
	// No leftover temp files in the dir.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if len(e.Name()) >= 4 && e.Name()[:4] == ".tmp" {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

func TestSaveCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "cache")
	c := Open(dir, "kiro")
	c.Set("a", "A")
	if err := c.Save(); err != nil {
		t.Fatalf("Save should create dir: %v", err)
	}
	if _, err := os.Stat(c.Path()); err != nil {
		t.Errorf("cache file not created: %v", err)
	}
}
