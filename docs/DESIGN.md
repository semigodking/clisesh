# clisesh — Design

## Overview

`clisesh` browses and manages agent CLI conversation sessions across multiple
providers. The central design goal is **extensibility**: adding a new agent
CLI (Kiro, Antigravity, Codex, Claude Code, ...) must require only a new
provider implementation — no changes to the UI, configuration, theming,
caching, or summarization layers.

## Architecture

Dependencies point inward toward `domain`. Outer layers (providers, TUI,
summarizers) depend on the domain abstractions, never the reverse.

```
                 ┌───────────────────────────┐
                 │        cmd/clisesh         │  wiring / CLI entrypoint
                 └──────────────┬────────────┘
                                │
        ┌───────────────┬───────┴───────┬────────────────┐
        ▼               ▼               ▼                ▼
   ┌─────────┐    ┌──────────┐    ┌──────────┐    ┌────────────┐
   │  tui    │    │  config  │    │  theme   │    │ summarize  │
   └────┬────┘    └────┬─────┘    └────┬─────┘    └─────┬──────┘
        │              │               │                │
        └──────────────┴───────┬───────┴────────────────┘
                               ▼
                        ┌─────────────┐
                        │   domain    │  Session, Capabilities,
                        │             │  SessionProvider (interface)
                        └──────┬──────┘
                               ▲
              ┌────────────────┴─────────────────┐
              │             provider             │
              │  registry → kiro, antigravity    │
              └──────────────────────────────────┘
```

### Packages

| Package               | Responsibility                                                    |
|-----------------------|-------------------------------------------------------------------|
| `internal/domain`     | `Session`, `Capabilities`, `SessionProvider` interface. No deps.  |
| `internal/textutil`   | CJK-aware display width, truncation, wrapping, age formatting.    |
| `internal/provider`   | Provider registry + concrete providers (`kiro`, `antigravity`, `claude`, `codex`, `aggregate`). |
| `internal/summarize`  | `Summarizer` interface + backends (`llm`, `subprocess`).          |
| `internal/cache`      | Per-provider, versioned description cache.                        |
| `internal/config`     | TOML + env + flags layered configuration.                         |
| `internal/theme`      | Color roles → Lipgloss styles; named themes + overrides.          |
| `internal/tui`        | Bubble Tea model/update/view; depends only on domain/theme/config.|
| `cmd/clisesh`         | Entrypoint; wires config-selected provider/summarizer/theme.      |

## Core model

```go
type Session struct {
    ID, ProviderID, Title, Description, CWD string
    UpdatedAt time.Time
    Locked    bool
    ParentID  string
    Raw       map[string]any
}

type Capabilities struct {
    HasTitle, HasParent, CanResume, CanDelete, CascadeDelete, ProvidesPrompts bool
}

type SessionProvider interface {
    ID() string
    Capabilities() Capabilities
    List(ctx) ([]Session, error)
    UserPrompts(ctx, id string, max int) ([]string, error)
    Resume(ctx, id string) error
    Delete(ctx, ids []string) (deleted []string, err error)
    Children(ctx, id string) ([]string, error)
}
```

### Why capability flags?

Real agent CLIs store and expose sessions very differently (see
[capability matrix](providers/capability-matrix.md)). Some have titles (Kiro),
some don't (Antigravity). Some link subagents (Kiro `parent_session_id`),
some don't. Rather than force every provider to fake unsupported features,
each advertises `Capabilities()` and the UI hides or disables actions
accordingly. This keeps providers honest and the UI robust.

## Summarization

`Summarizer` is an interface with two primary backends:

- **subprocess** — shells out to configured CLI agents (e.g. `kiro-cli`, `agy`) in non-interactive mode. Multiple named subprocesses can be defined under `[summarizer.subprocess.<name>]`.
- **llm** — HTTP chat-completions API (OpenAI, DeepSeek, GLM, Ollama, llama.cpp, etc.) with configurable base URL, API key, model, temperature, timeout, format, and custom headers. Multiple named LLMs can be defined under `[summarizer.llm.<name>]`.

### Key Mechanisms:
- **Worker Pool Concurrency (`batch_size`)**: In the TUI, `batch_size` (default: 5) defines the capacity of the concurrent worker pool. Up to `batch_size` sessions run concurrently (`Summarizing…`) while the remainder wait in a FIFO queue (`Queued for summary…`).
- **1-by-1 Continuous Streaming**: As soon as any individual session completes, its row immediately updates with the summary in the UI and cache, and a waiting task is promoted from the queue into the pool.
- **API Rate Limiting (RPM Control)**: When using LLM APIs, an internal sliding window rate limiter (`llm.Limiter`) strictly controls outgoing requests so they never exceed the configured requests-per-minute (`rpm`) limit over any rolling 60-second window.
- **Prompt Smart Extraction**: Extracts initial intent turns (`head_prompts`) and latest progress turns (`tail_prompts`), scrubbing code blocks (` ```...``` ` -> `[code]`) to reduce token consumption and noise.
- **Failure State Preservation**: If summarization fails or is aborted due to an error, sessions revert to their exact original pre-summarize descriptions (precedence: snapshot > cache > title > NoDescription), avoiding accidental data loss to `(no description)`.
- **Manual Description Editing**: Users can press `m` on any session to open an interactive modal dialog and directly edit/override the description. Confirmed changes update in-memory session rows and are persisted immediately to the disk cache.

## Configuration

Layered precedence: **CLI flags > environment (`CLISESH_*`) > TOML file >
built-in defaults**. File location is XDG-aware
(`~/.config/clisesh/config.toml`). See [002-config](specs/002-config.md).

## Theming

Colors are expressed as semantic **roles** (cursor, marked, header, footer,
status, age, lock, cwd, warning, title, key) mapped to Lipgloss styles. Built-in
themes (`default`, `gray`, `high-contrast`, `256`) can be selected by name and
individual roles (and provider badge colors) overridden in config. See [003-theme](specs/003-theme.md).

## Caching

Descriptions are cached per provider at
`~/.cache/clisesh/<provider>-descriptions.json`, versioned so format changes
invalidate stale entries. Writes are atomic; deleted sessions are purged. See
[004-cache](specs/004-cache.md).

## Documentation map

- Per-component specs: [`docs/specs/`](specs/)
- Decisions: [`docs/adr/`](adr/)
- Provider formats + capabilities: [`docs/providers/capability-matrix.md`](providers/capability-matrix.md)
