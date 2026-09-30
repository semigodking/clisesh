# ADR 0003: TOML configuration with layered precedence

- Status: Accepted
- Date: 2026-09-24

## Context

The tool needs rich, human-editable configuration (active provider, theme,
summarizer backend + credentials, title length, cache location) with the
ability to override per-invocation.

## Decision

Use a **TOML** file at `~/.config/clisesh/config.toml` (XDG-aware via
`XDG_CONFIG_HOME`), layered as **CLI flags > environment (`CLISESH_*`) > file
> built-in defaults**.

## Rationale

- TOML is idiomatic in the Go ecosystem and friendlier than JSON for humans
  (comments, sections).
- Layering supports both persistent config and quick per-run overrides.
- Env-var support enables referencing secrets (API keys) without committing
  them to the file.

## Consequences

- One TOML decoder dependency (`BurntSushi/toml` or `pelletier/go-toml`).
- API keys may be given directly or via `${ENV_VAR}` indirection; keys are
  redacted in `config print` and never logged.
