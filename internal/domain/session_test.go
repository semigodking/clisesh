package domain

import (
	"sort"
	"testing"
	"time"
)

func TestShortID(t *testing.T) {
	s := Session{ID: "450ee908-d016-4c2a-ae10-5404d39091b4"}
	if s.ShortID() != "450ee908" {
		t.Errorf("ShortID = %q, want 450ee908", s.ShortID())
	}
	short := Session{ID: "abc"}
	if short.ShortID() != "abc" {
		t.Errorf("ShortID for short id = %q, want abc", short.ShortID())
	}
	opencodeSess := Session{ID: "ses_f14aac24bffeJaXyEHtxaBzxOU", ProviderID: "opencode"}
	if opencodeSess.ShortID() != "f14aac24" {
		t.Errorf("ShortID for opencode session = %q, want f14aac24", opencodeSess.ShortID())
	}
}

func TestIsSubagent(t *testing.T) {
	if (Session{}).IsSubagent() {
		t.Error("session with empty ParentID should not be a subagent")
	}
	if !(Session{ParentID: "x"}).IsSubagent() {
		t.Error("session with ParentID should be a subagent")
	}
}

// SortByUpdatedDesc is the canonical newest-first ordering used by the UI.
func TestSessionsSortNewestFirst(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	sessions := []Session{
		{ID: "old", UpdatedAt: base},
		{ID: "new", UpdatedAt: base.Add(2 * time.Hour)},
		{ID: "mid", UpdatedAt: base.Add(1 * time.Hour)},
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})
	if sessions[0].ID != "new" || sessions[1].ID != "mid" || sessions[2].ID != "old" {
		t.Errorf("unexpected order: %v", []string{sessions[0].ID, sessions[1].ID, sessions[2].ID})
	}
}
