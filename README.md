# clisesh

`clisesh` is a terminal UI designed to help developers **quickly find and restore previous agent CLI working environments**. If you frequently juggle multiple long-running tasks across different AI agent CLIs (Kiro, Antigravity, Claude Code, Codex, etc.), `clisesh` aggregates all your active and historical sessions in one place, lets you instantly search through them with AI-generated task summaries, and seamlessly resumes your workflow with a single keystroke.

## Features

- **Interactive TUI (Bubble Tea)**: Browse sessions with AI-generated descriptions, human-readable relative ages, working directory accents, and real-time filtering.
- **Manual Description Editing**: Press `m` on any session to manually edit, customize, or override its description directly in a modal dialog. Persisted immediately to disk cache.
- **Multiple Providers Behind One Interface**: Aggregates sessions across **Kiro**, **Antigravity (`agy`)**, **Claude Code**, **OpenAI Codex**, and **OpenCode** in a unified, newest-first view.
- **Capability-Aware UI**: Automatically adapts hotkeys and visible hints based on what the active provider supports (`HasTitle`, `CanResume`, `CanDelete`, `CascadeDelete`, `ProvidesPrompts`).
- **Active Session Safety**: Live lock detection (`●`) prevents accidental deletion of running sessions across all providers (skips active sessions in batch deletes).
- **Branded & High-Contrast Theming**:
  - Provider signature badges (`AGY` blue, `KIR` purple, `CLD` terracotta orange, `CDX` mint green, etc.).
  - Full row highlight inheritance: active lock and brand badges retain signature colors without breaking cursor/marked background bars.
  - Fully customizable via semantic color roles and per-provider overrides.
- **Worker Pool & Streaming TUI Updates**:
  - Concurrency pool sized to `batch_size` (default: 5 concurrent workers).
  - Fine-grained 1-by-1 streaming: as soon as any single session finishes, its row updates immediately and the next queued session is automatically promoted to `Summarizing…`.
  - Dynamic status bar shows active in-flight count, queue length, and progress.
- **API Rate Limiting (RPM Control)**:
  - Enforces strict maximum requests-per-minute limits over a rolling 60-second sliding window for HTTP LLM backends.
  - Configurable globally or per-backend (`rpm = 30`), via env (`CLISESH_SUMMARIZER_RPM`), or via `--rpm` flag.
- **Configurable Smart Summarization**:
  - **Head + Tail Selection**: Automatically extracts initial goal turns (head) and latest progress turns (tail), skipping repetitive middle debugging cycles.
  - **Code Noise Scrubbing**: Strips large markdown code fences (` ```...``` ` -> `[code]`) and caps single prompts to keep input concise and tokens low (~60% token reduction).
  - **Subprocess & LLM Backends**: CLI subprocesses (`kiro-cli`, `agy`) or HTTP APIs (OpenAI, Ollama, DeepSeek, GLM, etc.).
- **Debounced & Resilient Lifecycle**:
  - **Failure Preservation**: If re-summarization fails, existing descriptions are preserved and restored rather than overwritten with placeholder or `(no description)`.
  - In-flight summary deduplication prevents duplicate runs on repeated keypresses.
  - Safe concurrent deletion: tracks surviving sessions and suppresses stale overwrites.
  - Scrollable error dialog modal (`e`) for detailed failure inspection.
- **Layered Configuration**: CLI flags > environment variables (`CLISESH_*`) > TOML config file > defaults.

## Install

```bash
git clone https://github.com/semigodking/clisesh
cd clisesh
make build          # produces bin/clisesh (static binary)
```

## Usage

```bash
clisesh                       # launch the TUI with the configured provider (default: all)
clisesh tui --provider kiro   # launch the TUI for a specific provider
clisesh list --provider all   # list sessions across all providers (non-interactive)
clisesh summarize [id...]     # generate & cache descriptions
clisesh providers             # list available providers (kiro, antigravity, claude, codex, opencode)
clisesh theme list            # list color themes
clisesh theme preview gray    # preview a theme's color roles
clisesh cache stats           # show cached description counts
clisesh config print          # show effective configuration
clisesh config init           # write a default config file (--force to overwrite)
clisesh version               # print version
clisesh help                  # full usage
```

### Keyboard Shortcuts in TUI

| Key | Action | Note |
| :--- | :--- | :--- |
| `↑` / `k`, `↓` / `j` | Move cursor | Wraps or clamps within list bounds |
| `g` / `home`, `G` / `end` | Jump to top / bottom | Quick navigation |
| `pgup` / `pgdown` | Page up / down | Fast scrolling |
| `space` | Toggle mark | Multi-select sessions |
| `enter` | Resume session | Replaces process with target CLI (`kiro-cli`, `agy`, `claude`, `codex`, `opencode`) |
| `m` | Edit description | Manually edits description in a modal dialog and saves to cache |
| `r` | Re-summarize | Queues selected (or cursor) session for background summarization |
| `d` | Delete session | Prompts for confirmation; active sessions (`●`) are protected |
| `i` | Session info | Scrollable popup displaying ID, provider, age, CWD, and parent |
| `e` | View error | Opens scrollable dialog when summarization fails |
| `/` | Filter | Real-time filter across title, description, and ID (`esc` clears) |
| `q` / `ctrl+c` | Quit | Gracefully cancels background workers and exits |

---

### Providers

`clisesh` aggregates all installed agent CLIs by default (`provider = "all"`). Each row displays an active lock glyph (`●`), multi-select mark (`✓`), 3-letter brand badge, relative age, short ID, and description:

| Provider | ID | Default Directory | Active Lock Detection | Resume Command |
| :--- | :--- | :--- | :--- | :--- |
| **Kiro** | `kiro` | `~/.kiro/sessions/cli/` | `<uuid>.lock` existence | `kiro-cli chat --resume-id <id>` |
| **Antigravity** | `antigravity` / `agy` | `~/.gemini/antigravity-cli/brain/` | `flock` on `presence/<uuid>.lock` | `agy --conversation <id>` |
| **Claude Code** | `claude` | `~/.claude/projects/` | PID check in `~/.claude/sessions/` | `claude --resume <id>` |
| **OpenAI Codex** | `codex` | `~/.codex/sessions/` | Active process & session rollout check | `codex resume <id>` |
| **OpenCode** | `opencode` | `~/.local/share/opencode/` + service | Active sessions via `/api/session/active` | `opencode --session <id>` |

Select `all` (default) or a specific provider via config (`provider = "..."`), environment variable (`CLISESH_PROVIDER=...`), or `--provider <name>`.

---

### Theming & Colors

Themes map semantic roles onto Lipgloss styles. Shipped themes: `default`, `gray`, `high-contrast`, `256`.

You can customize individual role colors or provider badge colors under `[theme_overrides]` in `config.toml`:

```toml
theme = "default"

[theme_overrides]
cursor_fg = "black"              # High-contrast text on highlight
cursor_bg = "yellow"             # Highlight bar color
marked_fg = "black"              # Selected text color
marked_bg = "magenta"            # Selected row color
lock_fg = "green"                # Active session lock glyph (●)
footer_fg = "black"              # Footer text foreground color
footer_bg = "white"              # Footer background color
key_fg = "white"                 # Key label / shortcut badges foreground color
key_bg = "black"                 # Key label / shortcut badges background color

# Custom provider badge colors
provider_antigravity_fg = "brightblue"
provider_kiro_fg = "magenta"
provider_claude_fg = "brightred"
provider_codex_fg = "green"
```

---

### Summarization Configuration

Configure how sessions are summarized under `[summarizer]` in `config.toml`:

```toml
[summarizer]
backend = "subprocess.kiro"      # or "subprocess.agy", "llm.openai", "llm.deepseek", etc.
model = "qwen3-coder-next"
title_len = 80
timeout_sec = 60

# --- Worker Pool & Rate Limiting Controls ---
batch_size = 5                   # Concurrent worker pool capacity (default: 5)
rpm = 30                         # Max API requests per minute for LLM backends (0 = unlimited)

# --- Prompt Extraction Controls ---
max_prompts = 10                 # Max user prompts extracted from session history (default: 10)
head_prompts = 2                 # Number of initial prompts to capture task intent (default: 2)
tail_prompts = 1                 # Number of latest prompts to capture final outcome (default: 1)
prompt_max_chars = 400           # Character limit per individual prompt (default: 400)
max_chars = 1500                 # Character limit per session in combined prompt (default: 1500)

[summarizer.subprocess.kiro]
command = "kiro-cli"
args = ["chat", "--no-interactive", "--model", "{model}", "--effort", "low", "--wrap", "never", "{prompt}"]

[summarizer.llm.openai]
base_url = "https://api.openai.com/v1"
api_key = "${OPENAI_API_KEY}"
model = "gpt-4o-mini"
temperature = 0.2
timeout_sec = 60
rpm = 60                         # Per-backend RPM override
```

All summarizer parameters can also be configured via environment variables:
- `CLISESH_SUMMARIZER_BACKEND`
- `CLISESH_SUMMARIZER_MODEL`
- `CLISESH_SUMMARIZER_BATCH_SIZE`
- `CLISESH_SUMMARIZER_RPM`
- `CLISESH_SUMMARIZER_MAX_PROMPTS`
- `CLISESH_SUMMARIZER_HEAD_PROMPTS`
- `CLISESH_SUMMARIZER_TAIL_PROMPTS`

## Adding a provider

1. Create `internal/provider/<name>/<name>.go` implementing
   `domain.SessionProvider` and returning appropriate `Capabilities`.
2. Register it in an `init()` via `provider.Register("<name>", factory)`.
3. Add a blank import in `cmd/clisesh/main.go` so the init runs.
4. Document its storage format and capabilities in
   [`docs/providers/capability-matrix.md`](docs/providers/capability-matrix.md)
   and a `docs/specs/00N-provider-<name>.md`.

No changes to the domain model, TUI, config, theming, cache, or summarizer are
required — see [ADR 0002](docs/adr/0002-provider-capability-flags.md).


## Documentation

- [Design overview](docs/DESIGN.md)
- [Specs](docs/specs/) — one numbered spec per component
- [Provider capability matrix](docs/providers/capability-matrix.md)
- [Architecture Decision Records](docs/adr/)

## Development

```bash
make test     # run all tests
make lint     # go vet + gofmt check
make cover    # coverage summary
make build    # static binary
```

Requires Go 1.22+.

## License

MIT
