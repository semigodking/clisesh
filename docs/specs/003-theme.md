# Spec 003 — Theme Engine

Status: Implemented (Task 3)

## Purpose

Express UI colors as semantic **roles** rather than hardcoded values, so users
can select a named theme or override individual roles via config. Roles are
resolved into Lipgloss styles the TUI consumes.

## Color roles

| Role       | Used for                                    |
|------------|---------------------------------------------|
| `cursor`   | The highlighted (selected) row              |
| `marked`   | Rows marked for a bulk action               |
| `header`   | Top title bar                               |
| `footer`   | Bottom help/hints bar                       |
| `status`   | Status row (session id / counts)            |
| `age`      | Age column                                  |
| `lock`     | Active/locked indicator                     |
| `cwd`      | Working-directory text                      |
| `warning`  | Destructive confirmations & error status    |
| `title`    | Emphasis text (e.g. info popup headings)    |
| `key`      | Keyboard shortcut badge styling             |

Each role has a foreground and background color; background may be empty
(meaning "terminal default").

## Built-in themes

- `default` — blue header, yellow cursor, standard ANSI.
- `gray` — gray status row and muted accents (256-color grayscale ramp).
- `high-contrast` — bold black/white for accessibility.
- `256` — richer palette using xterm-256 indices.

## Color value formats

A color string in a theme or override may be:
- a named ANSI color: `black`, `red`, `green`, `yellow`, `blue`, `magenta`,
  `cyan`, `white`, `gray`, and `bright*` variants;
- an xterm-256 index: `"0".."255"`;
- a hex value: `"#rrggbb"`;
- empty `""` → terminal default (only meaningful for backgrounds).

Lipgloss/termenv degrades colors to the terminal's actual capability, so a
256/truecolor value renders as the nearest ANSI color on limited terminals.

## Overrides
 
`config.theme_overrides` is a `map[string]string` keyed by:
- `"<role>_fg"` / `"<role>_bg"` for standard UI roles (including `footer_fg/bg`, `key_fg/bg`)
- `"provider_<name>_fg"` for CLI provider badges (e.g. `provider_kiro_fg`, `provider_antigravity_fg`, `provider_opencode_fg`)

```toml
[theme_overrides]
status_fg = "250"
status_bg = "237"
cursor_bg = "yellow"
footer_fg = "black"
footer_bg = "white"
key_fg = "white"
key_bg = "black"
provider_kiro_fg = "magenta"
provider_antigravity_fg = "brightblue"
provider_claude_fg = "brightred"
provider_codex_fg = "green"
provider_opencode_fg = "brightmagenta"
```

Overrides are applied on top of the selected named theme. Unknown keys are
ignored; invalid color values fall back to the theme's or provider's default brand value.

## Provider Brand Colors

Default brand colors for CLI badges:
- **Kiro (`KIR`)**: Purple (`magenta` / `#9046FF`)
- **Antigravity (`AGY`)**: Google Gemini blue (`brightblue` / `#4285F4`)
- **Claude Code (`CLD`)**: Terracotta orange (`brightred` / `#DA7756`)
- **Codex (`CDX`)**: Mint green (`green` / `#10A37F`)
- **OpenCode (`OPC`)**: Purple / Magenta (`brightmagenta`)
- **DeepSeek (`DSK`)**: Ocean blue (`blue` / `#0066FF`)
- **GLM (`GLM`)**: Cobalt blue (`brightcyan` / `#345DFF`)
- **Cursor (`CRS`)**: Cyan (`cyan`)
- **Aider (`AID`)**: Amber yellow (`brightyellow`)

## API

```go
type Role int   // Cursor, Marked, Header, Footer, Status, Age, Lock, Cwd, Warning, Title, Key
type Theme struct { /* role -> colors */ }

func Names() []string
func Load(name string, overrides map[string]string) (Theme, error)
func (Theme) Style(role Role) lipgloss.Style
func (Theme) StyleWithBg(role Role, bg string) lipgloss.Style
func (Theme) ProviderColor(providerID string) lipgloss.Style
```

`Load` returns an error for an unknown theme name. Building styles never
errors (invalid overrides are dropped).

## Tests

- `Names()` includes the four built-ins.
- `Load` errors on unknown theme.
- Each built-in theme defines all roles (completeness).
- Overrides change the resolved style; invalid override falls back.
- Named/256/hex color parsing.
- Key & footer role overrides.

## Demo

`clisesh theme preview <name>` renders a swatch of every role.
