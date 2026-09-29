package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/semigodking/clisesh/internal/cache"
	"github.com/semigodking/clisesh/internal/config"
	"github.com/semigodking/clisesh/internal/provider"
	_ "github.com/semigodking/clisesh/internal/provider/antigravity"
	_ "github.com/semigodking/clisesh/internal/provider/kiro"
	"github.com/semigodking/clisesh/internal/summarize"
)

// TestEndToEndListSummarizeCache wires config -> provider -> summarizer ->
// cache against a Kiro fixture, using the title short-circuit so no external
// LLM is needed. It verifies the full pipeline that the TUI relies on.
func TestEndToEndListSummarizeCache(t *testing.T) {
	// Fixture Kiro session dir.
	dir := t.TempDir()
	id := "450ee908-d016-4c2a-ae10-5404d39091b4"
	os.WriteFile(filepath.Join(dir, id+".json"),
		[]byte(`{"title":"Investigate 403 errors","cwd":"/p","updated_at":"2026-01-02T10:00:00Z"}`), 0o644)
	// Non-empty transcript so the session is not treated as an empty session;
	// the title short-circuit still avoids any LLM call.
	os.WriteFile(filepath.Join(dir, id+".jsonl"),
		[]byte(`{"kind":"Prompt","data":{"content":[{"kind":"text","data":"why do I get 403 errors"}]}}`+"\n"), 0o644)

	cacheDir := t.TempDir()
	cfg := config.Default()
	cfg.Provider = "kiro"
	cfg.CacheDir = cacheDir

	prov, err := provider.New(cfg.Provider, dir)
	if err != nil {
		t.Fatalf("provider.New: %v", err)
	}
	summ, err := buildSummarizer(cfg)
	if err != nil {
		t.Fatalf("buildSummarizer: %v", err)
	}

	ctx := context.Background()
	sessions, err := prov.List(ctx)
	if err != nil || len(sessions) != 1 {
		t.Fatalf("List: %v, n=%d", err, len(sessions))
	}

	c := cache.Open(cfg.CacheDir, cfg.Provider)
	sessions = fillDescriptions(ctx, prov, summ, c, cfg, sessions)

	if sessions[0].Description != "Investigate 403 errors" {
		t.Errorf("description = %q, want title", sessions[0].Description)
	}
	// Cache should now contain the description and persist.
	if _, ok := c.Get(id); !ok {
		t.Error("description not cached")
	}
	c2 := cache.Open(cfg.CacheDir, cfg.Provider)
	if got, ok := c2.Get(id); !ok || got != "Investigate 403 errors" {
		t.Errorf("reopened cache Get = %q,%v", got, ok)
	}
}

func TestProvidersRegistered(t *testing.T) {
	names := provider.Names()
	has := map[string]bool{}
	for _, n := range names {
		has[n] = true
	}
	if !has["kiro"] || !has["antigravity"] || !has["claude"] || !has["codex"] || !has["opencode"] {
		t.Errorf("expected kiro, antigravity, claude, codex, and opencode registered, got %v", names)
	}
}

// A session with no title and no prompts is now classified as an empty
// session (decision 1a) and gets the EmptySession sentinel without an LLM call.
func TestFillDescriptionsFallback(t *testing.T) {
	dir := t.TempDir()
	id := "1ff13b30-4ec4-4224-9ec6-f825e06ecb44"
	// No title, no jsonl -> no prompts.
	os.WriteFile(filepath.Join(dir, id+".json"),
		[]byte(`{"updated_at":"2026-01-02T10:00:00Z"}`), 0o644)

	cfg := config.Default()
	cfg.Provider = "kiro"
	cfg.CacheDir = t.TempDir()
	prov, _ := provider.New("kiro", dir)
	summ, _ := buildSummarizer(cfg)
	ctx := context.Background()
	sessions, _ := prov.List(ctx)
	c := cache.Open(cfg.CacheDir, cfg.Provider)
	sessions = fillDescriptions(ctx, prov, summ, c, cfg, sessions)
	if len(sessions) != 1 || sessions[0].Description != summarize.EmptySession {
		t.Errorf("expected EmptySession sentinel, got %q", sessions[0].Description)
	}
}
