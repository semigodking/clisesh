package aggregate

import (
	"context"
	"testing"
	"time"

	"github.com/semigodking/clisesh/internal/domain"
)

// fake is a minimal in-memory provider for testing aggregation/routing.
type fake struct {
	id       string
	sessions []domain.Session
	caps     domain.Capabilities
	deleted  []string
	resumed  string
}

func (f *fake) ID() string                        { return f.id }
func (f *fake) Capabilities() domain.Capabilities { return f.caps }
func (f *fake) List(context.Context) ([]domain.Session, error) {
	return f.sessions, nil
}
func (f *fake) UserPrompts(_ context.Context, id string, _ int) ([]string, error) {
	return []string{f.id + ":" + id}, nil
}
func (f *fake) ValidateResume(_ context.Context, id string) error {
	return nil
}
func (f *fake) Resume(_ context.Context, id string) error {
	f.resumed = id
	return nil
}
func (f *fake) Delete(_ context.Context, ids []string) ([]string, error) {
	f.deleted = append(f.deleted, ids...)
	return ids, nil
}
func (f *fake) Children(context.Context, string) ([]string, error) { return nil, nil }

func build() (*Provider, *fake, *fake) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a := &fake{
		id:   "kiro",
		caps: domain.Capabilities{CanResume: true, CanDelete: true, CascadeDelete: true, HasTitle: true, ProvidesPrompts: true},
		sessions: []domain.Session{
			{ID: "k1", ProviderID: "kiro", UpdatedAt: base.Add(3 * time.Hour)},
			{ID: "k2", ProviderID: "kiro", UpdatedAt: base.Add(1 * time.Hour)},
		},
	}
	b := &fake{
		id:   "antigravity",
		caps: domain.Capabilities{CanResume: true, CanDelete: true, ProvidesPrompts: true},
		sessions: []domain.Session{
			{ID: "a1", ProviderID: "antigravity", UpdatedAt: base.Add(2 * time.Hour)},
		},
	}
	return New([]domain.SessionProvider{a, b}), a, b
}

func TestAggregateListMergedNewestFirst(t *testing.T) {
	agg, _, _ := build()
	sessions, err := agg.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 3 {
		t.Fatalf("want 3 merged sessions, got %d", len(sessions))
	}
	// Newest-first across providers: k1(3h), a1(2h), k2(1h).
	want := []string{"k1", "a1", "k2"}
	for i, id := range want {
		if sessions[i].ID != id {
			t.Errorf("pos %d = %s, want %s", i, sessions[i].ID, id)
		}
	}
}

func TestAggregateCapabilitiesUnion(t *testing.T) {
	agg, _, _ := build()
	c := agg.Capabilities()
	if !c.CascadeDelete {
		t.Error("union should include CascadeDelete (from kiro)")
	}
	if !c.CanResume || !c.CanDelete || !c.ProvidesPrompts || !c.HasTitle {
		t.Errorf("union capabilities wrong: %+v", c)
	}
}

func TestAggregateRoutesUserPrompts(t *testing.T) {
	agg, _, _ := build()
	p, _ := agg.UserPrompts(context.Background(), "a1", 5)
	if len(p) != 1 || p[0] != "antigravity:a1" {
		t.Errorf("UserPrompts routed wrong: %v", p)
	}
	p, _ = agg.UserPrompts(context.Background(), "k2", 5)
	if len(p) != 1 || p[0] != "kiro:k2" {
		t.Errorf("UserPrompts routed wrong: %v", p)
	}
}

func TestAggregateRoutesResume(t *testing.T) {
	agg, a, b := build()
	_ = agg.Resume(context.Background(), "k1")
	if a.resumed != "k1" {
		t.Errorf("kiro should have resumed k1, got %q", a.resumed)
	}
	if b.resumed != "" {
		t.Errorf("antigravity should not have resumed, got %q", b.resumed)
	}
}

func TestAggregateRoutesValidateResume(t *testing.T) {
	agg, _, _ := build()
	if err := agg.ValidateResume(context.Background(), "k1"); err != nil {
		t.Errorf("ValidateResume(k1) failed: %v", err)
	}
	if err := agg.ValidateResume(context.Background(), "nonexistent"); err != domain.ErrNotSupported {
		t.Errorf("ValidateResume(nonexistent) want ErrNotSupported, got %v", err)
	}
}

func TestAggregateDeleteGroupsByProvider(t *testing.T) {
	agg, a, b := build()
	deleted, err := agg.Delete(context.Background(), []string{"k1", "a1", "k2"})
	if err != nil {
		t.Fatal(err)
	}
	if len(deleted) != 3 {
		t.Errorf("want 3 deleted, got %d: %v", len(deleted), deleted)
	}
	// kiro got k1,k2; antigravity got a1.
	if len(a.deleted) != 2 {
		t.Errorf("kiro deleted = %v, want 2", a.deleted)
	}
	if len(b.deleted) != 1 || b.deleted[0] != "a1" {
		t.Errorf("antigravity deleted = %v, want [a1]", b.deleted)
	}
}

func TestAggregateUnknownIDSafe(t *testing.T) {
	agg, _, _ := build()
	if err := agg.Resume(context.Background(), "nonexistent"); err != domain.ErrNotSupported {
		t.Errorf("unknown id resume should be ErrNotSupported, got %v", err)
	}
	deleted, _ := agg.Delete(context.Background(), []string{"nonexistent"})
	if len(deleted) != 0 {
		t.Errorf("unknown id delete should be no-op, got %v", deleted)
	}
}

func TestAggregateID(t *testing.T) {
	agg, _, _ := build()
	if agg.ID() != "all" {
		t.Errorf("aggregate ID = %q, want all", agg.ID())
	}
}
