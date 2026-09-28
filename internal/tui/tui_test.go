package tui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/semigodking/clisesh/internal/domain"
	"github.com/semigodking/clisesh/internal/summarize"
	"github.com/semigodking/clisesh/internal/theme"
)

// fakeProvider is a test double with configurable capabilities.
type fakeProvider struct {
	caps              domain.Capabilities
	deleted           []string
	validateResumeErr error
}

func (f *fakeProvider) ID() string                        { return "fake" }
func (f *fakeProvider) Capabilities() domain.Capabilities { return f.caps }
func (f *fakeProvider) List(context.Context) ([]domain.Session, error) {
	return nil, nil
}
func (f *fakeProvider) UserPrompts(context.Context, string, int) ([]string, error) {
	return []string{"a prompt"}, nil
}
func (f *fakeProvider) ValidateResume(context.Context, string) error { return f.validateResumeErr }
func (f *fakeProvider) Resume(context.Context, string) error         { return nil }
func (f *fakeProvider) Delete(_ context.Context, ids []string) ([]string, error) {
	f.deleted = append(f.deleted, ids...)
	return ids, nil
}
func (f *fakeProvider) Children(context.Context, string) ([]string, error) { return nil, nil }

// fakeSummarizer returns a fixed description for every item.
type fakeSummarizer struct {
	desc   string
	err    error
	called bool
}

func (f *fakeSummarizer) Summarize(_ context.Context, items []summarize.Item) (map[string]string, error) {
	f.called = true
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]string{}
	for _, it := range items {
		out[it.ID] = f.desc
	}
	return out, nil
}

func testSessions() []domain.Session {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return []domain.Session{
		{ID: "aaaaaaaa-1", ProviderID: "fake", Title: "Alpha task", Description: "Alpha task", UpdatedAt: base.Add(3 * time.Hour)},
		{ID: "bbbbbbbb-2", ProviderID: "fake", Title: "Beta work", Description: "Beta work", UpdatedAt: base.Add(2 * time.Hour)},
		{ID: "cccccccc-3", ProviderID: "fake", Title: "Gamma job", Description: "Gamma job", UpdatedAt: base.Add(time.Hour)},
	}
}

func newModel(caps domain.Capabilities) (Model, *fakeProvider) {
	fp := &fakeProvider{caps: caps}
	th, _ := theme.Load("default", nil)
	m := New(fp, nil, nil, th, testSessions(), nil, nil, nil, nil)
	m.width = 80
	m.height = 20
	return m, fp
}

func key(s string) tea.KeyMsg {
	switch s {
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func allCaps() domain.Capabilities {
	return domain.Capabilities{HasTitle: true, HasParent: true, CanResume: true, CanDelete: true, CascadeDelete: true, ProvidesPrompts: true}
}

func TestCursorMovementBounds(t *testing.T) {
	m, _ := newModel(allCaps())
	// Up at top stays at 0.
	mm, _ := m.Update(key("up"))
	m = mm.(Model)
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0", m.cursor)
	}
	// Down three times clamps at last index (2).
	for i := 0; i < 5; i++ {
		mm, _ = m.Update(key("down"))
		m = mm.(Model)
	}
	if m.cursor != 2 {
		t.Errorf("cursor = %d, want 2 (clamped)", m.cursor)
	}

	// 'g' jumps to top
	mm, _ = m.Update(key("g"))
	m = mm.(Model)
	if m.cursor != 0 {
		t.Errorf("cursor after 'g' = %d, want 0", m.cursor)
	}

	// 'G' jumps to bottom
	mm, _ = m.Update(key("G"))
	m = mm.(Model)
	if m.cursor != 2 {
		t.Errorf("cursor after 'G' = %d, want 2", m.cursor)
	}

	// 'ctrl+u' jumps up
	mm, _ = m.Update(key("ctrl+u"))
	m = mm.(Model)
	if m.cursor != 0 {
		t.Errorf("cursor after 'ctrl+u' = %d, want 0", m.cursor)
	}

	// 'ctrl+d' jumps down
	mm, _ = m.Update(key("ctrl+d"))
	m = mm.(Model)
	if m.cursor != 2 {
		t.Errorf("cursor after 'ctrl+d' = %d, want 2", m.cursor)
	}
}

func TestMarkToggle(t *testing.T) {
	m, _ := newModel(allCaps())
	mm, _ := m.Update(key("space")) // marks aaaaaaaa-1, advances to index 1
	m = mm.(Model)
	if !m.marked["aaaaaaaa-1"] {
		t.Error("first session should be marked")
	}
	if m.cursor != 1 {
		t.Errorf("cursor should advance to 1, got %d", m.cursor)
	}
}

func TestEnterResumesWhenCapable(t *testing.T) {
	m, _ := newModel(allCaps())
	mm, cmd := m.Update(key("enter"))
	m = mm.(Model)
	if m.resumeID != "aaaaaaaa-1" {
		t.Errorf("resumeID = %q, want aaaaaaaa-1", m.resumeID)
	}
	if cmd == nil {
		t.Error("enter should return a quit command")
	}
}

func TestEnterDoesNotResumeWhenIncapable(t *testing.T) {
	caps := allCaps()
	caps.CanResume = false
	m, _ := newModel(caps)
	mm, _ := m.Update(key("enter"))
	m = mm.(Model)
	if m.resumeID != "" {
		t.Errorf("resumeID should be empty when CanResume=false, got %q", m.resumeID)
	}
}

func TestFilterMode(t *testing.T) {
	m, _ := newModel(allCaps())
	mm, _ := m.Update(key("/"))
	m = mm.(Model)
	if m.mode != modeFilter {
		t.Fatal("/ should enter filter mode")
	}
	// Type "beta".
	for _, ch := range []string{"b", "e", "t", "a"} {
		mm, _ = m.Update(key(ch))
		m = mm.(Model)
	}
	f := m.filtered()
	if len(f) != 1 || f[0].ID != "bbbbbbbb-2" {
		t.Errorf("filter 'beta' should match only Beta work, got %d", len(f))
	}
	// Enter applies and returns to normal.
	mm, _ = m.Update(key("enter"))
	m = mm.(Model)
	if m.mode != modeNormal {
		t.Error("enter should apply filter and return to normal mode")
	}
}

func TestDeleteGatedByCapability(t *testing.T) {
	// Without CanDelete, 'd' must not enter confirm mode.
	caps := allCaps()
	caps.CanDelete = false
	m, _ := newModel(caps)
	mm, _ := m.Update(key("d"))
	m = mm.(Model)
	if m.mode == modeConfirmDelete {
		t.Error("d should be a no-op when CanDelete=false")
	}
}

func TestDeleteFlow(t *testing.T) {
	m, fp := newModel(allCaps())
	// Mark first, then delete.
	mm, _ := m.Update(key("space"))
	m = mm.(Model)
	mm, _ = m.Update(key("d"))
	m = mm.(Model)
	if m.mode != modeConfirmDelete {
		t.Fatal("d should enter confirm mode")
	}
	mm, _ = m.Update(key("y"))
	m = mm.(Model)
	if len(fp.deleted) != 1 || fp.deleted[0] != "aaaaaaaa-1" {
		t.Errorf("provider.Delete called with %v, want [aaaaaaaa-1]", fp.deleted)
	}
	// Session removed from state.
	for _, s := range m.sessions {
		if s.ID == "aaaaaaaa-1" {
			t.Error("deleted session should be removed from model")
		}
	}
}

func TestDeleteActiveSessionBlocked(t *testing.T) {
	m, fp := newModel(allCaps())
	// Mark session as locked/active
	m.sessions[0].Locked = true

	// Pressing 'd' on active cursor session should not enter confirm mode and should set status
	mm, _ := m.Update(key("d"))
	m = mm.(Model)
	if m.mode == modeConfirmDelete {
		t.Fatal("d should NOT enter confirm mode when cursor session is active")
	}
	if !strings.Contains(m.status, "cannot delete active session") {
		t.Errorf("expected status 'cannot delete active session(s)', got %q", m.status)
	}
	if len(fp.deleted) != 0 {
		t.Errorf("expected no deletions, got %v", fp.deleted)
	}

	// Mark both active session 0 and inactive session 1
	m.marked["aaaaaaaa-1"] = true
	m.marked["bbbbbbbb-2"] = true
	mm, _ = m.Update(key("d"))
	m = mm.(Model)
	if m.mode != modeConfirmDelete {
		t.Fatal("d should enter confirm mode when there are deletable sessions among marked")
	}
	// Check confirm banner mentions active skipped
	footer := m.View()
	if !strings.Contains(footer, "active skipped") {
		t.Errorf("expected footer to mention 'active skipped', got:\n%s", footer)
	}

	// Confirm delete
	mm, _ = m.Update(key("y"))
	m = mm.(Model)
	if len(fp.deleted) != 1 || fp.deleted[0] != "bbbbbbbb-2" {
		t.Errorf("expected only inactive session bbbbbbbb-2 to be deleted, got %v", fp.deleted)
	}
}

func TestResummarizeUpdatesDescription(t *testing.T) {
	fp := &fakeProvider{caps: allCaps()}
	th, _ := theme.Load("default", nil)
	fs := &fakeSummarizer{desc: "freshly generated summary"}
	m := New(fp, fs, nil, th, testSessions(), nil, nil, nil, nil)
	m.width, m.height = 80, 20

	// Press 'r': should set placeholder immediately and return a command.
	mm, cmd := m.Update(key("r"))
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("re-summarize should return a background command")
	}
	if m.sessions[0].Description != summarizingPlaceholder {
		t.Errorf("description should show placeholder while working, got %q", m.sessions[0].Description)
	}

	// Run the background command to obtain its message, then feed it back.
	msg := cmd()
	if !fs.called {
		t.Fatal("summarizer should have been invoked by the command")
	}
	mm, _ = m.Update(msg)
	m = mm.(Model)
	if m.sessions[0].Description != "freshly generated summary" {
		t.Errorf("description not updated after completion: %q", m.sessions[0].Description)
	}
}

func TestInfoMode(t *testing.T) {
	m, _ := newModel(allCaps())
	mm, _ := m.Update(key("i"))
	m = mm.(Model)
	if m.mode != modeInfo {
		t.Fatal("i should enter info mode")
	}
	// Any key closes.
	mm, _ = m.Update(key("x"))
	m = mm.(Model)
	if m.mode != modeNormal {
		t.Error("any key should close info popup")
	}
}

func TestViewRendersContent(t *testing.T) {
	m, _ := newModel(allCaps())
	out := m.View()
	if !contains(out, "clisesh") {
		t.Error("view should render the header with app name")
	}
	if !contains(out, "Alpha task") {
		t.Error("view should render session descriptions")
	}
	if !contains(out, "space: mark") {
		t.Error("view should render footer hints")
	}
}

func TestRowAlignmentUniform(t *testing.T) {
	// Cursor row and a normal row must place the description at the same column.
	m, _ := newModel(allCaps())
	cursorRow := m.viewRow(m.sessions[0], true, 80)
	normalRow := m.viewRow(m.sessions[1], false, 80)
	// Strip ANSI so column math is on plain text.
	cIdx := indexOf(stripANSI(cursorRow), "[")
	nIdx := indexOf(stripANSI(normalRow), "[")
	if cIdx != nIdx {
		t.Errorf("shortid bracket column differs: cursor=%d normal=%d", cIdx, nIdx)
	}
}

func stripANSI(s string) string {
	var b []rune
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
			continue
		}
		b = append(b, r)
	}
	return string(b)
}

func TestProviderBadgeInRow(t *testing.T) {
	m, _ := newModel(allCaps())
	// fakeProvider.ID() == "fake" -> badge "FAK"; sessions carry ProviderID "fake".
	row := stripANSI(m.viewRow(m.sessions[0], false, 80))
	if !contains(row, " FAK ") {
		t.Errorf("row should contain provider badge 'FAK', got: %q", row)
	}
}

func TestRowBadgesRetainColorsOnCursorAndMarked(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	m, _ := newModel(allCaps())
	m.sessions[0].Locked = true
	m.sessions[0].ProviderID = "antigravity"

	// 1. Normal row
	normalRow := m.viewRow(m.sessions[0], false, 80)
	if !contains(normalRow, "●") || !contains(normalRow, "AGY") {
		t.Errorf("normal row missing glyph or badge: %q", normalRow)
	}
	// Active lock glyph must have ANSI styling applied
	if contains(normalRow, " ● ") {
		t.Errorf("lock glyph in normal row should be ANSI colored, not raw uncolored text")
	}

	// 2. Cursor row
	cursorRow := m.viewRow(m.sessions[0], true, 80)
	if !contains(cursorRow, "●") || !contains(cursorRow, "AGY") {
		t.Errorf("cursor row missing glyph or badge: %q", cursorRow)
	}
	// Verify cursor background is present in the rendered line
	cursorBgColor := m.theme.BgColor(theme.Cursor)
	if cursorBgColor == "" {
		t.Fatalf("expected Cursor role to have bg color")
	}

	// 3. Marked row
	m.marked[m.sessions[0].ID] = true
	markedRow := m.viewRow(m.sessions[0], false, 80)
	if !contains(markedRow, "●") || !contains(markedRow, "AGY") {
		t.Errorf("marked row missing glyph or badge: %q", markedRow)
	}

	// 4. Inactive session on cursor row: lock position is space, should not render ●
	m.sessions[0].Locked = false
	inactiveCursorRow := m.viewRow(m.sessions[0], true, 80)
	if contains(inactiveCursorRow, "●") {
		t.Errorf("inactive session should not display lock glyph ●")
	}
	if !contains(inactiveCursorRow, "AGY") {
		t.Errorf("inactive session on cursor row should still display provider badge AGY")
	}
}

func TestProviderBadge(t *testing.T) {
	cases := map[string]string{
		"kiro":        "KIR",
		"antigravity": "AGY",
		"agy":         "AGY",
		"claude":      "CLD",
		"codex":       "CDX",
		"cursor":      "CRS",
		"deepseek":    "DSK",
		"glm":         "GLM",
		"aider":       "AID",
		"opencode":    "OPC",
		"":            "   ",
	}
	for in, want := range cases {
		if got := ProviderBadge(in); got != want {
			t.Errorf("ProviderBadge(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInfoDialogHasBorder(t *testing.T) {
	m, _ := newModel(allCaps())
	m.mode = modeInfo
	out := stripANSI(m.View())
	// Rounded border corners should be present.
	if !contains(out, "╭") || !contains(out, "╰") {
		t.Errorf("info dialog should render a border box; got:\n%s", out)
	}
	if !contains(out, "Session Information") {
		t.Error("info dialog should contain the title")
	}
	if !contains(out, "Provider:") {
		t.Error("info dialog should show the provider field")
	}
}

func TestViewDoesNotPanic(t *testing.T) {
	m, _ := newModel(allCaps())
	_ = m.View()
	m.mode = modeInfo
	_ = m.View()
	m.mode = modeConfirmDelete
	_ = m.View()
	m.mode = modeFilter
	_ = m.View()
}

func TestFooterHidesDeleteWhenIncapable(t *testing.T) {
	caps := allCaps()
	caps.CanDelete = false
	m, _ := newModel(caps)
	footer := m.viewFooter(120)
	if contains(footer, "d: delete") {
		t.Error("footer should not advertise delete when CanDelete=false")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestAutoSummarizeOnInit(t *testing.T) {
	fp := &fakeProvider{caps: allCaps()}
	th, _ := theme.Load("default", nil)
	fs := &fakeSummarizer{desc: "auto-summarized text"}
	sessions := testSessions()
	sessions[0].Description = summarizingPlaceholder

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m := New(fp, fs, nil, th, sessions, []string{"aaaaaaaa-1"}, nil, ctx, cancel)
	m.width, m.height = 80, 20

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init should return background auto-summarize command when pendingSummaries is non-empty")
	}

	msg := cmd()
	if !fs.called {
		t.Fatal("summarizer should have been invoked by Init command")
	}

	mm, _ := m.Update(msg)
	m = mm.(Model)
	if m.sessions[0].Description != "auto-summarized text" {
		t.Errorf("session[0] description = %q, want auto-summarized text", m.sessions[0].Description)
	}
}

func TestResumeCancelsContext(t *testing.T) {
	fp := &fakeProvider{caps: allCaps()}
	th, _ := theme.Load("default", nil)
	cancelled := false
	ctx, cancel := context.WithCancel(context.Background())
	wrappedCancel := func() {
		cancelled = true
		cancel()
	}

	m := New(fp, nil, nil, th, testSessions(), nil, nil, ctx, wrappedCancel)
	m.width, m.height = 80, 20

	mm, cmd := m.Update(key("enter"))
	m = mm.(Model)
	if !cancelled {
		t.Error("enter (resume) must call cancel() to abort background work")
	}
	if m.resumeID != "aaaaaaaa-1" {
		t.Errorf("resumeID = %q, want aaaaaaaa-1", m.resumeID)
	}
	if cmd == nil {
		t.Error("enter should return quit command")
	}
}

func TestQuitCancelsContext(t *testing.T) {
	fp := &fakeProvider{caps: allCaps()}
	th, _ := theme.Load("default", nil)
	cancelled := false
	ctx, cancel := context.WithCancel(context.Background())
	wrappedCancel := func() {
		cancelled = true
		cancel()
	}

	m := New(fp, nil, nil, th, testSessions(), nil, nil, ctx, wrappedCancel)
	m.width, m.height = 80, 20

	mm, _ := m.Update(key("q"))
	m = mm.(Model)
	if !cancelled {
		t.Error("q (quit) must call cancel() to abort background work")
	}
	if !m.quitting {
		t.Error("quitting should be true")
	}
}

func TestSingleLineStatusError(t *testing.T) {
	m, _ := newModel(allCaps())
	multilineErr := errors.New("http 500:\n{\n  \"error\": \"internal error\",\n  \"details\": \"server crashed\"\n}")

	// Feed resummarizeDoneMsg with multiline error
	mm, _ := m.Update(resummarizeDoneMsg{
		ids: []string{"aaaaaaaa-1"},
		err: multilineErr,
	})
	m = mm.(Model)

	if m.lastErr != multilineErr.Error() {
		t.Errorf("lastErr = %q, want original multiline error", m.lastErr)
	}

	statusView := m.viewStatus(80)
	if strings.Contains(statusView, "\n") {
		t.Errorf("viewStatus should NEVER contain newlines, got:\n%s", statusView)
	}
	if !strings.Contains(statusView, "[e: view error]") {
		t.Errorf("viewStatus should contain error hint, got %q", statusView)
	}
}

func TestErrorModalScrolling(t *testing.T) {
	m, _ := newModel(allCaps())
	m.lastErr = "Line 1\nLine 2\nLine 3\nLine 4\nLine 5\nLine 6\nLine 7\nLine 8\nLine 9\nLine 10\nLine 11\nLine 12\nLine 13\nLine 14\nLine 15\nLine 16\nLine 17\nLine 18\nLine 19\nLine 20"

	// Press 'e' to enter error modal
	mm, _ := m.Update(key("e"))
	m = mm.(Model)
	if m.mode != modeError {
		t.Fatalf("m.mode = %v, want modeError", m.mode)
	}
	if m.errOffset != 0 {
		t.Errorf("initial errOffset = %d, want 0", m.errOffset)
	}

	// Press 'j' / 'down' to scroll down
	mm, _ = m.Update(key("j"))
	m = mm.(Model)
	if m.errOffset != 1 {
		t.Errorf("errOffset after 'j' = %d, want 1", m.errOffset)
	}

	// Press 'pgdown'
	mm, _ = m.Update(key("pgdown"))
	m = mm.(Model)
	if m.errOffset <= 1 {
		t.Errorf("errOffset after 'pgdown' should increase, got %d", m.errOffset)
	}

	// Press 'g' to jump to top
	mm, _ = m.Update(key("g"))
	m = mm.(Model)
	if m.errOffset != 0 {
		t.Errorf("errOffset after 'g' = %d, want 0", m.errOffset)
	}

	// Press 'G' to jump to bottom
	mm, _ = m.Update(key("G"))
	m = mm.(Model)
	if m.errOffset == 0 {
		t.Error("errOffset after 'G' should be non-zero")
	}

	// Press 'esc' to exit
	mm, _ = m.Update(key("esc"))
	m = mm.(Model)
	if m.mode != modeNormal {
		t.Errorf("mode after 'esc' = %v, want modeNormal", m.mode)
	}
}

func TestDuplicateResummarizeIgnored(t *testing.T) {
	fp := &fakeProvider{caps: allCaps()}
	th, _ := theme.Load("default", nil)
	fs := &fakeSummarizer{desc: "fresh summary"}
	m := New(fp, fs, nil, th, testSessions(), nil, nil, nil, nil)
	m.width, m.height = 80, 20

	// Trigger 'r' on cursor (session 0)
	mm, cmd1 := m.Update(key("r"))
	m = mm.(Model)
	if cmd1 == nil {
		t.Fatal("first 'r' should return a command")
	}
	if !m.summState.inFlight[m.sessions[0].ID] {
		t.Errorf("expected session 0 to be tracked in inFlightSummaries")
	}

	// Trigger 'r' again immediately on same session
	mm, cmd2 := m.Update(key("r"))
	m = mm.(Model)
	if cmd2 != nil {
		t.Errorf("duplicate 'r' on same session should return nil cmd, got %v", cmd2)
	}
	if m.status != "already summarizing selected session(s)" {
		t.Errorf("expected duplicate status, got %q", m.status)
	}

	// Finish the command
	msg := cmd1()
	mm, _ = m.Update(msg)
	m = mm.(Model)

	// inFlightSummaries should be cleared
	if m.summState.inFlight[m.sessions[0].ID] {
		t.Errorf("session 0 should no longer be in inFlightSummaries after completion")
	}
	if m.sessions[0].Description != "fresh summary" {
		t.Errorf("expected description to be updated, got %q", m.sessions[0].Description)
	}
}

func TestDeleteDuringInFlightResummarize(t *testing.T) {
	fp := &fakeProvider{caps: allCaps()}
	th, _ := theme.Load("default", nil)
	fs := &fakeSummarizer{desc: "summary for surviving"}
	m := New(fp, fs, nil, th, testSessions(), nil, nil, nil, nil)
	m.width, m.height = 80, 20
	m.SetBatchSize(2) // both sessions in same chunk so one cmd() covers both

	// Mark sessions 0 and 1
	m.marked["aaaaaaaa-1"] = true
	m.marked["bbbbbbbb-2"] = true

	// Trigger 'r'
	mm, cmd := m.Update(key("r"))
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("expected command from 'r'")
	}

	// Delete session 0 while task is in flight
	m.doDelete([]string{"aaaaaaaa-1"})
	if m.status != "deleted 1 session(s)" {
		t.Fatalf("expected delete status, got %q", m.status)
	}

	// Execute command and apply message
	msg := cmd()
	mm, _ = m.Update(msg)
	m = mm.(Model)

	// Deleted session 0 should NOT be resurrected
	for _, s := range m.sessions {
		if s.ID == "aaaaaaaa-1" {
			t.Errorf("deleted session aaaaaaaa-1 should not be resurrected in m.sessions")
		}
	}
	// Surviving session 1 should be updated
	if m.sessions[0].ID != "bbbbbbbb-2" || m.sessions[0].Description != "summary for surviving" {
		t.Errorf("surviving session bbbbbbbb-2 was not updated properly: %+v", m.sessions[0])
	}
	// Status should accurately state 1 session summarized, NOT 2
	if m.status != "summarized 1 session(s)" {
		t.Errorf("expected 'summarized 1 session(s)', got %q", m.status)
	}
}

func TestDeleteAllDuringInFlightResummarize(t *testing.T) {
	fp := &fakeProvider{caps: allCaps()}
	th, _ := theme.Load("default", nil)
	fs := &fakeSummarizer{desc: "summary"}
	m := New(fp, fs, nil, th, testSessions()[:1], nil, nil, nil, nil)
	m.width, m.height = 80, 20

	// Trigger 'r' on the single session
	mm, cmd := m.Update(key("r"))
	m = mm.(Model)

	// Delete that single session while task is in flight
	m.doDelete([]string{testSessions()[0].ID})
	if m.status != "deleted 1 session(s)" {
		t.Fatalf("expected delete status, got %q", m.status)
	}

	// Deliver completed message
	msg := cmd()
	mm, _ = m.Update(msg)
	m = mm.(Model)

	// Status should RETAIN delete status, not be overwritten with summarized
	if m.status != "deleted 1 session(s)" {
		t.Errorf("expected delete status to be retained, got %q", m.status)
	}
}

func TestResumeValidationFailureModal(t *testing.T) {
	fp := &fakeProvider{
		caps:              allCaps(),
		validateResumeErr: errors.New("claude (Claude Code CLI) not found on PATH: exec: \"claude\": executable file not found in $PATH\n\nInstall with: npm install -g @anthropic-ai/claude-code"),
	}
	th, _ := theme.Load("default", nil)
	m := New(fp, nil, nil, th, testSessions(), nil, nil, nil, nil)
	m.width, m.height = 100, 30

	// Pressing Enter when CLI is missing
	mm, cmd := m.Update(key("enter"))
	m = mm.(Model)

	// Must NOT quit
	if m.quitting {
		t.Errorf("expected quitting=false on validation error, got true")
	}
	if cmd != nil {
		t.Errorf("expected cmd=nil on validation error, got %v", cmd)
	}
	if m.resumeID != "" {
		t.Errorf("expected resumeID to remain empty, got %q", m.resumeID)
	}
	if m.mode != modeError {
		t.Errorf("expected mode=modeError, got %v", m.mode)
	}
	if !strings.Contains(m.status, "resume failed: CLI not found") {
		t.Errorf("expected status to mention resume failed, got %q", m.status)
	}
	if !strings.Contains(m.lastErr, "claude (Claude Code CLI) not found") {
		t.Errorf("expected lastErr to contain detailed error, got %q", m.lastErr)
	}

	// View output should render error dialog with dynamic title and error details
	view := m.View()
	if !strings.Contains(view, "Cannot Resume Session: CLI Not Found") {
		t.Errorf("expected view to contain error modal title, got:\n%s", view)
	}
	if !strings.Contains(view, "Install with: npm install -g @anthropic-ai/claude-code") {
		t.Errorf("expected view to contain installation instructions, got:\n%s", view)
	}

	// Pressing 'esc' or 'enter' or 'q' should close modal without losing session state
	mm, _ = m.Update(key("esc"))
	m = mm.(Model)
	if m.mode != modeNormal {
		t.Errorf("expected modeNormal after esc, got %v", m.mode)
	}
	if len(m.filtered()) != len(testSessions()) {
		t.Errorf("sessions corrupted after error modal: got %d", len(m.filtered()))
	}
}

func TestResumeSuccessQuitsWithResumeID(t *testing.T) {
	fp := &fakeProvider{caps: allCaps()}
	th, _ := theme.Load("default", nil)
	m := New(fp, nil, nil, th, testSessions(), nil, nil, nil, nil)
	m.width, m.height = 100, 30

	mm, cmd := m.Update(key("enter"))
	m = mm.(Model)

	if !m.quitting {
		t.Errorf("expected quitting=true on success")
	}
	if cmd == nil {
		t.Errorf("expected tea.Quit command on success")
	}
	if m.resumeID != testSessions()[0].ID {
		t.Errorf("expected resumeID=%q, got %q", testSessions()[0].ID, m.resumeID)
	}
}

func TestIncrementalSummarizeQueueAndProgressiveUpdates(t *testing.T) {
	fp := &fakeProvider{caps: allCaps()}
	th, _ := theme.Load("default", nil)
	fs := &fakeSummarizer{desc: "updated description"}
	sessions := testSessions() // 3 sessions: aaaaaaaa-1, bbbbbbbb-2, cccccccc-3
	m := New(fp, fs, nil, th, sessions, nil, nil, nil, nil)
	m.SetBatchSize(1) // 1-by-1 granular stream
	m.width, m.height = 100, 30

	// Mark sessions 0 and 1
	m.marked["aaaaaaaa-1"] = true
	m.marked["bbbbbbbb-2"] = true

	// Trigger 'r' (re-summarize)
	mm, cmd := m.Update(key("r"))
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("expected command from 'r'")
	}

	// Active session 0 should be "Summarizing…", session 1 should be "Queued for summary…"
	if m.sessions[0].Description != "Summarizing…" {
		t.Errorf("expected session 0 to be Summarizing…, got %q", m.sessions[0].Description)
	}
	if m.sessions[1].Description != "Queued for summary…" {
		t.Errorf("expected session 1 to be Queued for summary…, got %q", m.sessions[1].Description)
	}
	if !strings.Contains(m.status, "re-summarizing (1/2)") {
		t.Errorf("expected status to show chunk 1/2, got %q", m.status)
	}

	// Session 0 completes
	msg1 := cmd()
	mm, nextCmd := m.Update(msg1)
	m = mm.(Model)

	// Session 0 must be updated immediately!
	if m.sessions[0].Description != "updated description" {
		t.Errorf("expected session 0 to be immediately updated, got %q", m.sessions[0].Description)
	}
	// Session 1 must now be promoted from Queued to Summarizing…
	if m.sessions[1].Description != "Summarizing…" {
		t.Errorf("expected session 1 to transition to Summarizing…, got %q", m.sessions[1].Description)
	}
	if nextCmd == nil {
		t.Fatal("expected next command for chunk 2")
	}
	if !strings.Contains(m.status, "re-summarizing (2/2)") {
		t.Errorf("expected status to show chunk 2/2, got %q", m.status)
	}

	// Session 1 completes
	msg2 := nextCmd()
	mm, finalCmd := m.Update(msg2)
	m = mm.(Model)

	// Both sessions must have updated descriptions
	if m.sessions[0].Description != "updated description" {
		t.Errorf("expected session 0 to be updated, got %q", m.sessions[0].Description)
	}
	if m.sessions[1].Description != "updated description" {
		t.Errorf("expected session 1 to be updated, got %q", m.sessions[1].Description)
	}
	if finalCmd != nil {
		t.Errorf("expected no further command when queue is empty, got %v", finalCmd)
	}
	if m.status != "summarized 2 session(s)" {
		t.Errorf("expected 'summarized 2 session(s)', got %q", m.status)
	}
}

func TestDeleteQueuedSessionRemovesFromQueue(t *testing.T) {
	fp := &fakeProvider{caps: allCaps()}
	th, _ := theme.Load("default", nil)
	fs := &fakeSummarizer{desc: "updated description"}
	sessions := testSessions()
	m := New(fp, fs, nil, th, sessions, nil, nil, nil, nil)
	m.SetBatchSize(1)
	m.width, m.height = 100, 30

	m.marked["aaaaaaaa-1"] = true
	m.marked["bbbbbbbb-2"] = true

	mm, cmd := m.Update(key("r"))
	m = mm.(Model)
	if cmd == nil {
		t.Fatal("expected command from 'r'")
	}

	// Delete queued session 1 while session 0 is in flight
	m.doDelete([]string{"bbbbbbbb-2"})

	// Verify session 1 is removed from queue
	if len(m.summState.queue) != 0 {
		t.Errorf("expected queued session to be removed from queue, got %v", m.summState.queue)
	}

	// Finish session 0
	msg := cmd()
	mm, nextCmd := m.Update(msg)
	m = mm.(Model)

	// Since session 1 was deleted from queue, there should be no next chunk command
	if nextCmd != nil {
		t.Errorf("expected no next command since queue was emptied, got %v", nextCmd)
	}
	if m.sessions[0].Description != "updated description" {
		t.Errorf("expected session 0 to be updated, got %q", m.sessions[0].Description)
	}
}

func TestAutoSummarize5Sessions(t *testing.T) {
	fp := &fakeProvider{caps: allCaps()}
	th, _ := theme.Load("default", nil)
	fs := &fakeSummarizer{desc: "summarized"}

	sessions := []domain.Session{
		{ID: "s-1", Title: "S1", Description: "Queued for summary…"},
		{ID: "s-2", Title: "S2", Description: "Queued for summary…"},
		{ID: "s-3", Title: "S3", Description: "Queued for summary…"},
		{ID: "s-4", Title: "S4", Description: "Queued for summary…"},
		{ID: "s-5", Title: "S5", Description: "Queued for summary…"},
	}
	pending := []string{"s-1", "s-2", "s-3", "s-4", "s-5"}
	promptsCache := map[string][]string{
		"s-1": {"prompt 1"},
		"s-2": {"prompt 2"},
		"s-3": {"prompt 3"},
		"s-4": {"prompt 4"},
		"s-5": {"prompt 5"},
	}

	m := New(fp, fs, nil, th, sessions, pending, promptsCache, nil, nil)

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init returned nil cmd")
	}

	// After Init(), the shared pointer should immediately have progress status
	if m.statusText() != "summarizing (1/5): 1 session(s)…" {
		t.Errorf("statusText() after Init = %q, want 'summarizing (1/5): 1 session(s)…'", m.statusText())
	}
	if m.sessions[0].Description != "Summarizing…" {
		t.Errorf("session 0 description after Init = %q, want 'Summarizing…'", m.sessions[0].Description)
	}
	if m.sessions[1].Description != "Queued for summary…" {
		t.Errorf("session 1 description after Init = %q, want 'Queued for summary…'", m.sessions[1].Description)
	}

	for i := 1; i <= 5; i++ {
		msg := cmd()
		mm, nextCmd := m.Update(msg)
		m = mm.(Model)

		if i < 5 {
			if nextCmd == nil {
				t.Fatalf("chain broken after chunk %d: nextCmd is nil", i)
			}
			expectedStatus := fmt.Sprintf("summarizing (%d/5): 1 session(s)…", i+1)
			if m.statusText() != expectedStatus {
				t.Errorf("after chunk %d: statusText() = %q, want %q", i, m.statusText(), expectedStatus)
			}
			if m.sessions[i].Description != "Summarizing…" {
				t.Errorf("after chunk %d: session %d description = %q, want 'Summarizing…'", i, i, m.sessions[i].Description)
			}
		} else {
			if nextCmd != nil {
				t.Errorf("after final chunk, expected nil cmd, got %v", nextCmd)
			}
			if m.status != "summarized 5 session(s)" {
				t.Errorf("final status = %q, want 'summarized 5 session(s)'", m.status)
			}
		}
		cmd = nextCmd
	}

	for i, s := range m.sessions {
		if s.Description != "summarized" {
			t.Errorf("session %d final description = %q, want 'summarized'", i, s.Description)
		}
	}
}

func TestSummarizeQueueAbortsOnFailure(t *testing.T) {
	fp := &fakeProvider{caps: allCaps()}
	th, _ := theme.Load("default", nil)
	// Summarizer fails with executable not found error
	fs := &fakeSummarizer{err: errors.New("kiro-cli failed: exec: \"kiro-cli\": executable file not found in $PATH")}

	sessions := []domain.Session{
		{ID: "s-1", Title: "Session 1", Description: "Queued for summary…"},
		{ID: "s-2", Title: "Session 2", Description: "Queued for summary…"},
		{ID: "s-3", Title: "Session 3", Description: "Queued for summary…"},
	}
	pending := []string{"s-1", "s-2", "s-3"}
	promptsCache := map[string][]string{
		"s-1": {"prompt 1"},
		"s-2": {"prompt 2"},
		"s-3": {"prompt 3"},
	}

	m := New(fp, fs, nil, th, sessions, pending, promptsCache, nil, nil)

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init returned nil cmd")
	}

	// Session 1 is actively Summarizing…, sessions 2 and 3 are Queued for summary…
	if m.sessions[0].Description != "Summarizing…" {
		t.Errorf("session 0 description = %q, want 'Summarizing…'", m.sessions[0].Description)
	}
	if m.sessions[1].Description != "Queued for summary…" {
		t.Errorf("session 1 description = %q, want 'Queued for summary…'", m.sessions[1].Description)
	}
	if m.sessions[2].Description != "Queued for summary…" {
		t.Errorf("session 2 description = %q, want 'Queued for summary…'", m.sessions[2].Description)
	}

	// Execute chunk 1 (which fails)
	msg := cmd()
	mm, nextCmd := m.Update(msg)
	m = mm.(Model)

	// nextCmd must be nil because the queue was aborted on fatal error
	if nextCmd != nil {
		t.Errorf("expected queue to abort with nextCmd == nil, got %v", nextCmd)
	}

	// Status should report error and hint
	if !strings.HasPrefix(m.status, "summarize failed:") {
		t.Errorf("status = %q, want prefix 'summarize failed:'", m.status)
	}
	if !strings.Contains(m.lastErr, "executable file not found") {
		t.Errorf("lastErr = %q, want to contain 'executable file not found'", m.lastErr)
	}

	// All sessions must be restored back to Title (or NoDescription) and NOT stuck on placeholders
	if m.sessions[0].Description != "Session 1" {
		t.Errorf("session 0 description after failure = %q, want 'Session 1'", m.sessions[0].Description)
	}
	if m.sessions[1].Description != "Session 2" {
		t.Errorf("session 1 description after abort = %q, want 'Session 2'", m.sessions[1].Description)
	}
	if m.sessions[2].Description != "Session 3" {
		t.Errorf("session 2 description after abort = %q, want 'Session 3'", m.sessions[2].Description)
	}

	// Queue state should be reset
	if len(m.summState.queue) != 0 {
		t.Errorf("queue length = %d, want 0", len(m.summState.queue))
	}
}

func TestSummarizeAccurateDescriptionTransitionsAcrossBatches(t *testing.T) {
	fp := &fakeProvider{caps: allCaps()}
	th, _ := theme.Load("default", nil)
	fs := &fakeSummarizer{desc: "done-desc"}

	sessions := []domain.Session{
		{ID: "s-1", Title: "Title 1", Description: "Queued for summary…"},
		{ID: "s-2", Title: "Title 2", Description: "Queued for summary…"},
		{ID: "s-3", Title: "Title 3", Description: "Queued for summary…"},
	}
	pending := []string{"s-1", "s-2", "s-3"}
	promptsCache := map[string][]string{
		"s-1": {"p1"},
		"s-2": {"p2"},
		"s-3": {"p3"},
	}

	m := New(fp, fs, nil, th, sessions, pending, promptsCache, nil, nil)
	cmd := m.Init()

	// Initial: s-1 Summarizing…, s-2 Queued, s-3 Queued
	if m.sessions[0].Description != "Summarizing…" || m.sessions[1].Description != "Queued for summary…" || m.sessions[2].Description != "Queued for summary…" {
		t.Fatalf("unexpected descriptions after Init: s1=%q, s2=%q, s3=%q", m.sessions[0].Description, m.sessions[1].Description, m.sessions[2].Description)
	}

	// Step 1: Chunk 1 finishes -> s-1 is done, s-2 transitions to Summarizing…, s-3 remains Queued
	msg1 := cmd()
	mm, cmd2 := m.Update(msg1)
	m = mm.(Model)
	if cmd2 == nil {
		t.Fatal("expected cmd2 for batch 2")
	}
	if m.sessions[0].Description != "done-desc" {
		t.Errorf("s1 desc = %q, want done-desc", m.sessions[0].Description)
	}
	if m.sessions[1].Description != "Summarizing…" {
		t.Errorf("s2 desc = %q, want Summarizing…", m.sessions[1].Description)
	}
	if m.sessions[2].Description != "Queued for summary…" {
		t.Errorf("s3 desc = %q, want Queued for summary…", m.sessions[2].Description)
	}

	// Step 2: Chunk 2 finishes -> s-2 is done, s-3 transitions to Summarizing…
	msg2 := cmd2()
	mm, cmd3 := m.Update(msg2)
	m = mm.(Model)
	if cmd3 == nil {
		t.Fatal("expected cmd3 for batch 3")
	}
	if m.sessions[1].Description != "done-desc" {
		t.Errorf("s2 desc = %q, want done-desc", m.sessions[1].Description)
	}
	if m.sessions[2].Description != "Summarizing…" {
		t.Errorf("s3 desc = %q, want Summarizing…", m.sessions[2].Description)
	}

	// Step 3: Chunk 3 finishes -> s-3 is done, queue finished, status shows summarized 3 session(s)
	msg3 := cmd3()
	mm, cmdFinal := m.Update(msg3)
	m = mm.(Model)
	if cmdFinal != nil {
		t.Errorf("expected nil cmd after last batch, got %v", cmdFinal)
	}
	if m.sessions[2].Description != "done-desc" {
		t.Errorf("s3 desc = %q, want done-desc", m.sessions[2].Description)
	}
	if m.status != "summarized 3 session(s)" {
		t.Errorf("final status = %q, want 'summarized 3 session(s)'", m.status)
	}
}

type idEchoSummarizer struct{}

func (s *idEchoSummarizer) Summarize(_ context.Context, items []summarize.Item) (map[string]string, error) {
	out := map[string]string{}
	for _, it := range items {
		out[it.ID] = "done-" + it.ID
	}
	return out, nil
}

func TestWorkerPoolConcurrencyAndDynamicRefill(t *testing.T) {
	fp := &fakeProvider{caps: allCaps()}
	th, _ := theme.Load("default", nil)
	fs := &idEchoSummarizer{}

	sessions := []domain.Session{
		{ID: "s0", Title: "Session 0", Description: "Queued for summary…"},
		{ID: "s1", Title: "Session 1", Description: "Queued for summary…"},
		{ID: "s2", Title: "Session 2", Description: "Queued for summary…"},
		{ID: "s3", Title: "Session 3", Description: "Queued for summary…"},
		{ID: "s4", Title: "Session 4", Description: "Queued for summary…"},
	}
	pending := []string{"s0", "s1", "s2", "s3", "s4"}
	promptsCache := map[string][]string{
		"s0": {"p0"},
		"s1": {"p1"},
		"s2": {"p2"},
		"s3": {"p3"},
		"s4": {"p4"},
	}

	m := New(fp, fs, nil, th, sessions, pending, promptsCache, nil, nil)
	// Worker pool capacity = 2
	m.SetBatchSize(2)

	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init returned nil cmd")
	}

	// Pool capacity is 2: s0 and s1 must be Summarizing…, s2, s3, s4 must be Queued for summary…
	if m.sessions[0].Description != "Summarizing…" {
		t.Errorf("s0 desc = %q, want Summarizing…", m.sessions[0].Description)
	}
	if m.sessions[1].Description != "Summarizing…" {
		t.Errorf("s1 desc = %q, want Summarizing…", m.sessions[1].Description)
	}
	if m.sessions[2].Description != "Queued for summary…" {
		t.Errorf("s2 desc = %q, want Queued for summary…", m.sessions[2].Description)
	}
	if m.sessions[3].Description != "Queued for summary…" {
		t.Errorf("s3 desc = %q, want Queued for summary…", m.sessions[3].Description)
	}
	if m.sessions[4].Description != "Queued for summary…" {
		t.Errorf("s4 desc = %q, want Queued for summary…", m.sessions[4].Description)
	}

	batchMsg, ok := cmd().(tea.BatchMsg)
	if !ok || len(batchMsg) != 2 {
		t.Fatalf("expected BatchMsg with 2 commands, got %v", cmd())
	}
	cmdS0 := batchMsg[0]
	cmdS1 := batchMsg[1]

	// 1. Worker 0 (s0) finishes first
	msg0 := cmdS0()
	mm, nextCmdS2 := m.Update(msg0)
	m = mm.(Model)

	// s0 must be immediately updated!
	if m.sessions[0].Description != "done-s0" {
		t.Errorf("s0 desc = %q, want done-s0", m.sessions[0].Description)
	}
	// s1 is still in flight in worker slot 1
	if m.sessions[1].Description != "Summarizing…" {
		t.Errorf("s1 desc = %q, want Summarizing…", m.sessions[1].Description)
	}
	// s2 should now be refilled into slot 0 and marked Summarizing…!
	if m.sessions[2].Description != "Summarizing…" {
		t.Errorf("s2 desc = %q, want Summarizing…", m.sessions[2].Description)
	}
	// s3 and s4 remain queued
	if m.sessions[3].Description != "Queued for summary…" || m.sessions[4].Description != "Queued for summary…" {
		t.Errorf("s3=%q, s4=%q, want Queued for summary…", m.sessions[3].Description, m.sessions[4].Description)
	}
	if nextCmdS2 == nil {
		t.Fatal("expected nextCmdS2 for refilled slot s2")
	}

	// 2. Worker 1 (s1) finishes
	msg1 := cmdS1()
	mm, nextCmdS3 := m.Update(msg1)
	m = mm.(Model)

	// s1 must be immediately updated!
	if m.sessions[1].Description != "done-s1" {
		t.Errorf("s1 desc = %q, want done-s1", m.sessions[1].Description)
	}
	// s2 is still in flight
	if m.sessions[2].Description != "Summarizing…" {
		t.Errorf("s2 desc = %q, want Summarizing…", m.sessions[2].Description)
	}
	// s3 should now be refilled into slot 1 and marked Summarizing…!
	if m.sessions[3].Description != "Summarizing…" {
		t.Errorf("s3 desc = %q, want Summarizing…", m.sessions[3].Description)
	}
	// s4 remains queued
	if m.sessions[4].Description != "Queued for summary…" {
		t.Errorf("s4 desc = %q, want Queued for summary…", m.sessions[4].Description)
	}
	if nextCmdS3 == nil {
		t.Fatal("expected nextCmdS3 for refilled slot s3")
	}

	// 3. Worker 0 (s2) finishes
	msg2 := nextCmdS2()
	mm, nextCmdS4 := m.Update(msg2)
	m = mm.(Model)

	if m.sessions[2].Description != "done-s2" {
		t.Errorf("s2 desc = %q, want done-s2", m.sessions[2].Description)
	}
	// s3 is still in flight
	if m.sessions[3].Description != "Summarizing…" {
		t.Errorf("s3 desc = %q, want Summarizing…", m.sessions[3].Description)
	}
	// s4 is refilled into slot 0 and marked Summarizing…
	if m.sessions[4].Description != "Summarizing…" {
		t.Errorf("s4 desc = %q, want Summarizing…", m.sessions[4].Description)
	}
	if nextCmdS4 == nil {
		t.Fatal("expected nextCmdS4 for refilled slot s4")
	}

	// 4. Worker 1 (s3) finishes -> queue is empty, so no new command returned
	msg3 := nextCmdS3()
	mm, emptyCmd := m.Update(msg3)
	m = mm.(Model)

	if m.sessions[3].Description != "done-s3" {
		t.Errorf("s3 desc = %q, want done-s3", m.sessions[3].Description)
	}
	if emptyCmd != nil {
		t.Errorf("expected nil cmd because queue was empty, got %v", emptyCmd)
	}
	// s4 is still in flight
	if m.sessions[4].Description != "Summarizing…" {
		t.Errorf("s4 desc = %q, want Summarizing…", m.sessions[4].Description)
	}

	// 5. Worker 0 (s4) finishes -> all 5 sessions done!
	msg4 := nextCmdS4()
	mm, finalCmd := m.Update(msg4)
	m = mm.(Model)

	if m.sessions[4].Description != "done-s4" {
		t.Errorf("s4 desc = %q, want done-s4", m.sessions[4].Description)
	}
	if finalCmd != nil {
		t.Errorf("expected finalCmd to be nil, got %v", finalCmd)
	}
	if m.status != "summarized 5 session(s)" {
		t.Errorf("status = %q, want 'summarized 5 session(s)'", m.status)
	}

	// Verify all sessions have their finished descriptions
	for i, s := range m.sessions {
		expected := fmt.Sprintf("done-s%d", i)
		if s.Description != expected {
			t.Errorf("session %d desc = %q, want %q", i, s.Description, expected)
		}
	}
}

