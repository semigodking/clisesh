# Spec 005 — Kiro Provider

Status: Implemented (Task 5)

## Purpose

Implement `domain.SessionProvider` for the Kiro CLI (`kiro-cli`).

## Storage layout

Base dir: `~/.kiro/sessions/cli/` (overridable for testing via `New(baseDir)`).

Per session `<uuid>`:
- `<uuid>.json` — metadata: `title`, `cwd`, `updated_at` (or `created_at`),
  `parent_session_id`.
- `<uuid>.jsonl` — transcript; user prompts are lines with `"kind":"Prompt"`.
- `<uuid>.lock` — present while the session is active/live.
- `<uuid>.history` — command history (deleted with the session).

## Capabilities

All true: `HasTitle`, `HasParent`, `CanResume`, `CanDelete`, `CascadeDelete`,
`ProvidesPrompts`.

## Behavior

### List
- Scan `*.json`, skipping dotfiles.
- Validate the stem against the UUID regex
  `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`.
- Parse metadata into `domain.Session` (ProviderID = "kiro").
- `Locked` = `<uuid>.lock` exists.
- **Exclude subagent sessions** (those with a non-empty `parent_session_id`).
- Sort newest-first by `UpdatedAt`.

### UserPrompts(id, max)
- Scan the full `<id>.jsonl` line by line.
- Fast pre-filter: only JSON-parse lines containing `"kind":"Prompt"`
  (skips huge tool-result / assistant lines cheaply).
- For each prompt, concatenate `data.content[]` items where `kind == "text"`.
- Return up to `max` prompts in order.

### Resume(id)
- Replace the process with `kiro-cli chat --resume-id <id>` (exec).

### ValidateResume(id)
- Verify `kiro-cli` binary exists on system `PATH` via `exec.LookPath("kiro-cli")`.

### Children(id)
- Return IDs of sessions whose `parent_session_id == id`.

### Delete(ids)
- For each id: also collect its children (cascade), then remove
  `<id>.json`, `.jsonl`, `.lock`, `.history` for the id and every child.
- Return the full set of IDs actually removed.
- Validate every id against the UUID regex before touching the filesystem.

## Registry

`internal/provider/registry.go` maps provider name → factory
(`func(cfg) (domain.SessionProvider, error)`) so `main` can construct the
configured provider by name. Kiro registers itself as `"kiro"`.
