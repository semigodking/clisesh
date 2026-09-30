# Agent Rules for clisesh

## Project Overview
`clisesh` is a terminal UI (TUI) for browsing, summarizing, resuming, and deleting conversation sessions across multiple agent CLIs (Kiro, Antigravity, Claude Code, OpenAI Codex, OpenCode).

## Core Architecture Principles
1. **Extensibility via Domain Abstraction**: Outer layers (`tui`, `summarize`, `theme`, `cache`, `config`) depend only on `domain` abstractions (`Session`, `Capabilities`, `SessionProvider`). Adding a new CLI provider must only require implementing `domain.SessionProvider` in `internal/provider/<name>/` — never modifying domain, TUI, config, or cache logic.
2. **Capability-Gated UI**: The UI must never fake unsupported features. Each provider advertises its `Capabilities()` (`HasTitle`, `HasParent`, `CanResume`, `CanDelete`, `CascadeDelete`, `ProvidesPrompts`), and the UI automatically enables, disables, or hides actions accordingly.
3. **Active Session Safety**: Never delete active/locked sessions. Running sessions must be protected during both single and batch deletion.
4. **No Legacy References**: Do not reference or introduce legacy Python-era predecessors in any user-facing documentation or new code.

## Documentation Maintenance Rules (Spec-Driven Workflow)
Whenever implementing new features, modifying capabilities, or refactoring components:
1. **Spec Sync is Mandatory**:
   - If adding a new Provider: Create a corresponding specification in `docs/specs/00N-provider-<name>.md` and register its capabilities/storage in `docs/providers/capability-matrix.md`.
   - If changing UI interactions / hotkeys: Update `docs/specs/007-tui.md`, `README.md`, and `README.zh-CN.md`.
   - If adding configuration options: Update `docs/specs/002-config.md` and `configs/config.example.toml`.
   - If changing summarizer / worker pool / rate limiting: Update `docs/specs/006-summarizer.md`.
   - If changing theme colors / roles: Update `docs/specs/003-theme.md`.
2. **Dual-Language README Maintenance**:
   - Every user-facing feature or CLI command change in `README.md` must be mirrored in `README.zh-CN.md`.
   - Keep technical implementation details out of the README files; link to `docs/` instead.
3. **Pre-Commit Verification**:
   - Run `make test` and `make lint` before concluding tasks to ensure all Go code and tests remain sound.
