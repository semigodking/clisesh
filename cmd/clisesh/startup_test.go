package main

import (
	"context"
	"testing"

	"github.com/semigodking/clisesh/internal/cache"
	"github.com/semigodking/clisesh/internal/domain"
	"github.com/semigodking/clisesh/internal/summarize"
)

// countingProvider is a test double that records UserPrompts calls and returns
// a configurable prompt set per id.
type countingProvider struct {
	prompts map[string][]string
	calls   map[string]int
}

func newCountingProvider(p map[string][]string) *countingProvider {
	return &countingProvider{prompts: p, calls: map[string]int{}}
}

func (f *countingProvider) ID() string { return "fake" }
func (f *countingProvider) Capabilities() domain.Capabilities {
	return domain.Capabilities{ProvidesPrompts: true}
}
func (f *countingProvider) List(context.Context) ([]domain.Session, error) {
	return nil, nil
}
func (f *countingProvider) UserPrompts(_ context.Context, id string, _ int) ([]string, error) {
	f.calls[id]++
	return f.prompts[id], nil
}
func (f *countingProvider) ValidateResume(context.Context, string) error { return nil }
func (f *countingProvider) Resume(context.Context, string) error         { return nil }
func (f *countingProvider) Delete(_ context.Context, ids []string) ([]string, error) {
	return ids, nil
}
func (f *countingProvider) Children(context.Context, string) ([]string, error) { return nil, nil }

func TestClassifyDescriptions(t *testing.T) {
	ctx := context.Background()
	c := cache.Open(t.TempDir(), "fake")
	// (a) cached EmptySession but prov now returns prompts -> scheduled.
	c.Set("a", summarize.EmptySession)
	// (b) cached EmptySession and prov still returns nothing -> stays empty.
	c.Set("b", summarize.EmptySession)
	// (c) normal cached desc -> trusted, no prompt fetch.
	c.Set("c", "Trusted description")

	prov := newCountingProvider(map[string][]string{
		"a": {"now has a prompt"},
		"b": nil,
		"c": {"should never be fetched"},
	})

	sessions := []domain.Session{
		{ID: "a", Title: "A title"},
		{ID: "b", Title: "B title"},
		{ID: "c", Title: "C title"},
	}

	out, missing := classifyDescriptions(ctx, prov, c, sessions)

	// (a) appears in missing.
	if len(missing) != 1 || missing[0].ID != "a" {
		t.Fatalf("missing = %+v, want exactly id 'a'", missing)
	}
	// (b) description == EmptySession and not scheduled.
	byID := map[string]domain.Session{}
	for _, s := range out {
		byID[s.ID] = s
	}
	if byID["b"].Description != summarize.EmptySession {
		t.Errorf("b desc = %q, want EmptySession", byID["b"].Description)
	}
	// (c) trusted description, no UserPrompts call.
	if byID["c"].Description != "Trusted description" {
		t.Errorf("c desc = %q, want trusted", byID["c"].Description)
	}
	if prov.calls["c"] != 0 {
		t.Errorf("UserPrompts called %d times for trusted 'c', want 0", prov.calls["c"])
	}
	// 'a' description left empty (pending summarization).
	if byID["a"].Description != "" {
		t.Errorf("a desc = %q, want empty pending", byID["a"].Description)
	}
}

func TestApplyDescriptionsSafetyNet(t *testing.T) {
	c := cache.Open(t.TempDir(), "fake")
	sessions := []domain.Session{
		{ID: "a", Title: "A title"},         // gets a generated desc
		{ID: "b", Title: "B title"},         // no desc -> falls back to title
		{ID: "d"},                           // no desc, no title -> NoDescription
		{ID: "e", Description: "prefilled"}, // already set (e.g. EmptySession-style)
	}
	descs := map[string]string{"a": "Generated A"}
	out := applyDescriptions(sessions, descs, c)

	byID := map[string]domain.Session{}
	for _, s := range out {
		byID[s.ID] = s
	}
	if byID["a"].Description != "Generated A" {
		t.Errorf("a = %q", byID["a"].Description)
	}
	if got, _ := c.Get("a"); got != "Generated A" {
		t.Errorf("cache a = %q", got)
	}
	if byID["b"].Description != "B title" {
		t.Errorf("b = %q, want title fallback", byID["b"].Description)
	}
	if byID["d"].Description != summarize.NoDescription {
		t.Errorf("d = %q, want NoDescription", byID["d"].Description)
	}
	if byID["e"].Description != "prefilled" {
		t.Errorf("e = %q, want prefilled preserved", byID["e"].Description)
	}
}
