package theme

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestNamesIncludesBuiltins(t *testing.T) {
	got := Names()
	want := map[string]bool{"default": false, "gray": false, "high-contrast": false, "256": false}
	for _, n := range got {
		if _, ok := want[n]; ok {
			want[n] = true
		}
	}
	for n, seen := range want {
		if !seen {
			t.Errorf("Names() missing built-in theme %q", n)
		}
	}
}

func TestLoadUnknownTheme(t *testing.T) {
	if _, err := Load("does-not-exist", nil); err == nil {
		t.Error("expected error for unknown theme")
	}
}

func TestThemeCompleteness(t *testing.T) {
	for _, name := range Names() {
		th, err := Load(name, nil)
		if err != nil {
			t.Fatalf("Load(%q): %v", name, err)
		}
		for _, role := range AllRoles() {
			if _, ok := th.roles[role]; !ok {
				t.Errorf("theme %q missing role %q", name, RoleName(role))
			}
		}
	}
}

func TestOverridesApplied(t *testing.T) {
	th, err := Load("default", map[string]string{
		"status_fg": "250",
		"status_bg": "237",
	})
	if err != nil {
		t.Fatal(err)
	}
	if th.roles[Status].fg != "250" || th.roles[Status].bg != "237" {
		t.Errorf("override not applied: %+v", th.roles[Status])
	}
}

func TestInvalidOverrideFallsBack(t *testing.T) {
	base, _ := Load("default", nil)
	th, _ := Load("default", map[string]string{"status_fg": "not-a-color"})
	if th.roles[Status].fg != base.roles[Status].fg {
		t.Errorf("invalid override should fall back to theme value; got %q", th.roles[Status].fg)
	}
}

func TestUnknownOverrideKeyIgnored(t *testing.T) {
	// Should not panic or error; key simply ignored.
	if _, err := Load("default", map[string]string{"bogus_key": "1", "nope": "x"}); err != nil {
		t.Errorf("unknown keys should be ignored, got %v", err)
	}
}

func TestValidColor(t *testing.T) {
	cases := map[string]bool{
		"":            true,
		"red":         true,
		"gray":        true,
		"255":         true,
		"0":           true,
		"256":         false,
		"-1":          false,
		"#aabbcc":     true,
		"#zzzzzz":     true, // length/prefix accepted; lipgloss handles rendering
		"not-a-color": false,
	}
	for in, want := range cases {
		if got := validColor(in); got != want {
			t.Errorf("validColor(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestStyleDoesNotPanic(t *testing.T) {
	th, _ := Load("gray", nil)
	for _, role := range AllRoles() {
		_ = th.Style(role) // building a style must never panic
	}
}

func TestProviderColorDefault(t *testing.T) {
	th, err := Load("default", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Verify known providers render non-empty styled text with their brand colors
	tests := []struct {
		provider string
		text     string
	}{
		{"kiro", "KIR"},
		{"antigravity", "AGY"},
		{"claude", "CLD"},
		{"codex", "CDX"},
		{"cursor", "CRS"},
		{"deepseek", "DSK"},
		{"glm", "GLM"},
		{"aider", "AID"},
		{"opencode", "OPC"},
	}

	for _, tt := range tests {
		style := th.ProviderColor(tt.provider)
		rendered := style.Render(tt.text)
		if rendered == "" {
			t.Errorf("ProviderColor(%q).Render(%q) should not be empty", tt.provider, tt.text)
		}
	}
}

func TestProviderColorOverride(t *testing.T) {
	th, err := Load("default", map[string]string{
		"provider_kiro_fg":   "#123456",
		"provider_custom_fg": "yellow",
	})
	if err != nil {
		t.Fatal(err)
	}

	if th.providerColors["kiro"] != "#123456" {
		t.Errorf("provider_kiro_fg override = %q, want #123456", th.providerColors["kiro"])
	}
	if th.providerColors["custom"] != "yellow" {
		t.Errorf("provider_custom_fg override = %q, want yellow", th.providerColors["custom"])
	}

	// Unknown provider without override returns default style without panic
	unreg := th.ProviderColor("unknown-tool")
	if unreg.Render("UNK") != "UNK" {
		t.Errorf("unregistered provider should render plain text when no color assigned")
	}
}

func TestThemeBgAndColorWithBg(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	th, err := Load("default", map[string]string{
		"cursor_bg": "yellow",
		"cursor_fg": "black",
	})
	if err != nil {
		t.Fatal(err)
	}

	if bg := th.BgColor(Cursor); bg != "yellow" {
		t.Errorf("BgColor(Cursor) = %q, want yellow", bg)
	}

	lockWithBg := th.LockStyleWithBg("yellow").Render("●")
	if lockWithBg == "●" || lockWithBg == "" {
		t.Errorf("LockStyleWithBg should render styled string with ANSI escapes, got %q", lockWithBg)
	}

	badgeWithBg := th.ProviderColorWithBg("antigravity", "yellow").Render("AGY")
	if badgeWithBg == "AGY" || badgeWithBg == "" {
		t.Errorf("ProviderColorWithBg should render styled string with ANSI escapes, got %q", badgeWithBg)
	}
}

func TestKeyRoleOverridesAndStyleWithBg(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	th, err := Load("default", map[string]string{
		"key_fg": "brightcyan",
		"key_bg": "235",
	})
	if err != nil {
		t.Fatal(err)
	}

	if th.roles[Key].fg != "brightcyan" {
		t.Errorf("key_fg = %q, want brightcyan", th.roles[Key].fg)
	}
	if th.roles[Key].bg != "235" {
		t.Errorf("key_bg = %q, want 235", th.roles[Key].bg)
	}

	rendered := th.KeyStyleWithBg("").Render("Enter")
	if rendered == "Enter" || rendered == "" {
		t.Errorf("KeyStyleWithBg should render styled key, got %q", rendered)
	}

	// Test fallback background when key_bg is empty
	thNoBg, err := Load("default", map[string]string{
		"key_fg": "yellow",
		"key_bg": "",
	})
	if err != nil {
		t.Fatal(err)
	}
	renderedWithFallback := thNoBg.KeyStyleWithBg("236").Render("ESC")
	if renderedWithFallback == "ESC" || renderedWithFallback == "" {
		t.Errorf("KeyStyleWithBg with fallback bg should render styled key, got %q", renderedWithFallback)
	}
}
