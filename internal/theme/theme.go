// Package theme maps semantic UI color roles onto Lipgloss styles. It ships
// named built-in themes and supports per-role overrides from configuration.
// Color values may be ANSI names, xterm-256 indices, or hex; termenv degrades
// them to the terminal's real capability at render time.
package theme

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Role enumerates the semantic color roles the UI uses.
type Role int

const (
	Cursor Role = iota
	Marked
	Header
	Footer
	Status
	Age
	Lock
	Cwd
	Warning
	Title
	Key
)

// roleNames maps a Role to its string key (used for overrides like
// "status_fg"). Order matters only for stable iteration in tests.
var roleNames = map[Role]string{
	Cursor:  "cursor",
	Marked:  "marked",
	Header:  "header",
	Footer:  "footer",
	Status:  "status",
	Age:     "age",
	Lock:    "lock",
	Cwd:     "cwd",
	Warning: "warning",
	Title:   "title",
	Key:     "key",
}

// AllRoles returns every role in a stable order.
func AllRoles() []Role {
	roles := make([]Role, 0, len(roleNames))
	for r := range roleNames {
		roles = append(roles, r)
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i] < roles[j] })
	return roles
}

// RoleName returns the string key for a role (e.g. "status").
func RoleName(r Role) string { return roleNames[r] }

// colorPair holds a foreground and (optional) background color string.
type colorPair struct {
	fg, bg string
	bold   bool
}

// Theme holds a color pair per role and custom provider badge colors.
type Theme struct {
	name           string
	roles          map[Role]colorPair
	providerColors map[string]string // providerID (normalized) -> foreground color string
}

// Name returns the theme's name.
func (t Theme) Name() string { return t.name }

// builtins defines all shipped themes. Every theme must specify all roles;
// completeness is enforced by a test.
var builtins = map[string]map[Role]colorPair{
	"default": {
		Cursor:  {fg: "black", bg: "yellow", bold: true},
		Marked:  {fg: "black", bg: "magenta"},
		Header:  {fg: "white", bg: "blue", bold: true},
		Footer:  {fg: "black", bg: "white"},
		Status:  {fg: "250", bg: "237"}, // gray status row (matches python)
		Age:     {fg: "yellow"},
		Lock:    {fg: "green"},
		Cwd:     {fg: "cyan"},
		Warning: {fg: "red", bold: true},
		Title:   {fg: "white", bold: true},
		Key:     {fg: "black", bg: "white", bold: true},
	},
	"gray": {
		Cursor:  {fg: "black", bg: "yellow", bold: true},
		Marked:  {fg: "black", bg: "cyan"},
		Header:  {fg: "250", bg: "237", bold: true},
		Footer:  {fg: "250", bg: "236"},
		Status:  {fg: "250", bg: "237"},
		Age:     {fg: "245"},
		Lock:    {fg: "green"},
		Cwd:     {fg: "244"},
		Warning: {fg: "red", bold: true},
		Title:   {fg: "252", bold: true},
		Key:     {fg: "white", bg: "238", bold: true},
	},
	"high-contrast": {
		Cursor:  {fg: "black", bg: "white", bold: true},
		Marked:  {fg: "white", bg: "black", bold: true},
		Header:  {fg: "black", bg: "white", bold: true},
		Footer:  {fg: "white", bg: "black", bold: true},
		Status:  {fg: "black", bg: "white"},
		Age:     {fg: "white", bold: true},
		Lock:    {fg: "white", bold: true},
		Cwd:     {fg: "white"},
		Warning: {fg: "white", bg: "red", bold: true},
		Title:   {fg: "white", bold: true},
		Key:     {fg: "black", bg: "white", bold: true},
	},
	"256": {
		Cursor:  {fg: "16", bg: "220", bold: true},
		Marked:  {fg: "16", bg: "141"},
		Header:  {fg: "231", bg: "24", bold: true},
		Footer:  {fg: "231", bg: "238"},
		Status:  {fg: "231", bg: "24"},
		Age:     {fg: "179"},
		Lock:    {fg: "78"},
		Cwd:     {fg: "80"},
		Warning: {fg: "231", bg: "160", bold: true},
		Title:   {fg: "231", bold: true},
		Key:     {fg: "220", bg: "236", bold: true},
	},
}

// Names returns the sorted list of built-in theme names.
func Names() []string {
	out := make([]string, 0, len(builtins))
	for n := range builtins {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// defaultProviderColors maps known agent CLI provider IDs to their signature brand color.
var defaultProviderColors = map[string]string{
	"antigravity": "brightblue",    // Google blue #4285F4
	"agy":         "brightblue",
	"kiro":        "magenta",       // Kiro purple #9046FF
	"kiro-cli":    "magenta",
	"claude":      "brightred",     // Anthropic terracotta orange #DA7756
	"claude-code": "brightred",
	"codex":       "green",         // OpenAI mint green #10A37F
	"cursor":      "cyan",          // Cursor cyan #00E5FF
	"deepseek":    "blue",          // DeepSeek ocean blue #0066FF / #1F4E79
	"dsk":         "blue",
	"glm":         "brightcyan",    // Zhipu cobalt blue #345DFF
	"zhipu":       "brightcyan",
	"aider":       "brightyellow",  // Aider amber yellow #F59E0B
	"opencode":    "brightmagenta", // OpenCode purple/magenta #A855F7
}

// Load returns the named theme with the given per-role overrides applied.
// Returns an error for an unknown theme name. Invalid override values are
// ignored (the theme's own value is kept).
func Load(name string, overrides map[string]string) (Theme, error) {
	base, ok := builtins[name]
	if !ok {
		return Theme{}, fmt.Errorf("unknown theme %q (have: %v)", name, Names())
	}
	roles := make(map[Role]colorPair, len(base))
	for r, cp := range base {
		roles[r] = cp
	}
	providerColors := make(map[string]string)
	applyOverrides(roles, providerColors, overrides)
	return Theme{name: name, roles: roles, providerColors: providerColors}, nil
}

// applyOverrides mutates roles using keys like "<role>_fg" / "<role>_bg"
// and populates providerColors using keys like "provider_<name>_fg".
func applyOverrides(roles map[Role]colorPair, providerColors map[string]string, overrides map[string]string) {
	if len(overrides) == 0 {
		return
	}
	// Build name->role lookup.
	byName := map[string]Role{}
	for r, n := range roleNames {
		byName[n] = r
	}
	for key, val := range overrides {
		if !validColor(val) {
			continue // invalid value: keep default
		}
		// Check for provider badge color override: "provider_<name>_fg"
		if strings.HasPrefix(key, "provider_") && strings.HasSuffix(key, "_fg") {
			providerName := strings.TrimSuffix(strings.TrimPrefix(key, "provider_"), "_fg")
			if providerName != "" {
				providerColors[strings.ToLower(providerName)] = val
				continue
			}
		}

		role, field, ok := splitOverrideKey(key, byName)
		if !ok {
			continue // unknown key: ignore
		}
		cp := roles[role]
		if field == "fg" {
			cp.fg = val
		} else {
			cp.bg = val
		}
		roles[role] = cp
	}
}

// splitOverrideKey parses "<role>_<fg|bg>" into its parts.
func splitOverrideKey(key string, byName map[string]Role) (Role, string, bool) {
	if len(key) < 4 {
		return 0, "", false
	}
	suffix := key[len(key)-3:]
	if suffix != "_fg" && suffix != "_bg" {
		return 0, "", false
	}
	name := key[:len(key)-3]
	role, ok := byName[name]
	if !ok {
		return 0, "", false
	}
	return role, suffix[1:], true
}

// Style builds the Lipgloss style for a role.
func (t Theme) Style(role Role) lipgloss.Style {
	cp := t.roles[role]
	s := lipgloss.NewStyle()
	if c := parseColor(cp.fg); c != nil {
		s = s.Foreground(c)
	}
	if c := parseColor(cp.bg); c != nil {
		s = s.Background(c)
	}
	if cp.bold {
		s = s.Bold(true)
	}
	return s
}

// BgColor returns the configured background color string for a role, or "" if unset.
func (t Theme) BgColor(role Role) string {
	return t.roles[role].bg
}

// LockStyleWithBg returns the Lipgloss style for the lock glyph with the given background color.
func (t Theme) LockStyleWithBg(bg string) lipgloss.Style {
	s := t.Style(Lock)
	if bg != "" {
		if c := parseColor(bg); c != nil {
			s = s.Background(c)
		}
	}
	return s
}

// KeyStyleWithBg returns the Lipgloss style for hotkey labels. If the Key role's
// background is unset and fallbackBg is provided, fallbackBg is applied as background.
func (t Theme) KeyStyleWithBg(fallbackBg string) lipgloss.Style {
	cp := t.roles[Key]
	s := lipgloss.NewStyle()
	if c := parseColor(cp.fg); c != nil {
		s = s.Foreground(c)
	}
	if cp.bg != "" {
		if c := parseColor(cp.bg); c != nil {
			s = s.Background(c)
		}
	} else if fallbackBg != "" {
		if c := parseColor(fallbackBg); c != nil {
			s = s.Background(c)
		}
	}
	if cp.bold {
		s = s.Bold(true)
	}
	return s
}

// ProviderColor returns the Lipgloss style for a provider's badge.
// It checks user theme_overrides first (e.g. "provider_kiro_fg"), then falls
// back to the provider's signature brand color, and finally to white/normal.
func (t Theme) ProviderColor(providerID string) lipgloss.Style {
	return t.ProviderColorWithBg(providerID, "")
}

// ProviderColorWithBg returns the Lipgloss style for a provider's badge with the given background color.
func (t Theme) ProviderColorWithBg(providerID string, bg string) lipgloss.Style {
	key := strings.ToLower(strings.TrimSpace(providerID))
	s := lipgloss.NewStyle().Bold(true)
	if bg != "" {
		if c := parseColor(bg); c != nil {
			s = s.Background(c)
		}
	}

	// 1. User config override
	if col, ok := t.providerColors[key]; ok {
		if c := parseColor(col); c != nil {
			return s.Foreground(c)
		}
	}
	// 2. Default brand color
	if col, ok := defaultProviderColors[key]; ok {
		if c := parseColor(col); c != nil {
			return s.Foreground(c)
		}
	}
	// 3. Fallback
	return s
}

// namedANSI maps human color names to ANSI-16 indices.
var namedANSI = map[string]string{
	"black": "0", "red": "1", "green": "2", "yellow": "3",
	"blue": "4", "magenta": "5", "cyan": "6", "white": "7",
	"gray": "8", "grey": "8",
	"brightred": "9", "brightgreen": "10", "brightyellow": "11",
	"brightblue": "12", "brightmagenta": "13", "brightcyan": "14",
	"brightwhite": "15",
}

// parseColor converts a color string to a lipgloss.Color, or nil for empty
// (terminal default). Accepts names, "0".."255", and "#rrggbb".
func parseColor(s string) lipgloss.TerminalColor {
	if s == "" {
		return nil
	}
	if idx, ok := namedANSI[s]; ok {
		return lipgloss.Color(idx)
	}
	return lipgloss.Color(s) // "#hex" or numeric index handled by lipgloss
}

// validColor reports whether s is a usable color value.
func validColor(s string) bool {
	if s == "" {
		return true // valid: means "default background"
	}
	if _, ok := namedANSI[s]; ok {
		return true
	}
	if len(s) == 7 && s[0] == '#' {
		return true
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 0 && n <= 255 {
		return true
	}
	return false
}
