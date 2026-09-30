# Spec 008 — Antigravity Provider

Status: Implemented (Task 10)

## Purpose

Implement `domain.SessionProvider` for Google's Antigravity CLI (`agy`). This
provider deliberately differs from Kiro (directory-per-conversation, no title,
different resume/delete) to prove the abstraction supports new CLIs without
changing domain, tui, summarize, or cache.

## Storage layout

Base dir: `~/.gemini/antigravity-cli/brain/` (overridable via `New(baseDir)`).

One directory per conversation, named with a UUID:
```
brain/<uuid>/.system_generated/logs/transcript.jsonl
```

There is **no metadata file** — no stored title or cwd. Timestamps come from
the transcript file's modification time.

Transcript entries (one JSON object per line) include:
- `USER_INPUT` — the user's messages.
- `PLANNER_RESPONSE` — the assistant's messages.

## Capabilities

```
HasTitle:        false   // no title recorded
HasParent:       false   // no subagent linkage
CanResume:       true    // agy --conversation <id>
CanDelete:       true    // remove the conversation directory
CascadeDelete:   false
ProvidesPrompts: true    // USER_INPUT entries
```

## Behavior

### List
- Read entries under `brain/`; each subdirectory named as a UUID is a session.
- `UpdatedAt` = mtime of the transcript file (fallback: dir mtime).
- `Locked` = whether `presence/<id>.lock` is actively locked via flock by a running `agy` process.
- `Title` and `CWD` are empty; the app relies on summarization.
- Newest-first by `UpdatedAt`.

### UserPrompts(id, max)
- Read `<id>/.system_generated/logs/transcript.jsonl` line by line.
- Parse entries; collect text from those of type `USER_INPUT`.
- Handle both a top-level `text` field and a `content`/`message` text shape
  defensively (the format is undocumented and evolving).
- Return up to `max`.

### Resume(id)
- Replace the process with `agy --conversation <id>` (exec).

### ValidateResume(id)
- Verify `agy` binary exists on system `PATH` via `exec.LookPath("agy")`.

### Delete(ids)
- For each id, validate strictly (non-empty, no `.`/`..`, no path separators,
  UUID-shaped) then `os.RemoveAll(brain/<id>)`.
- Never operate on the `brain/` root. Return the ids removed.

### Children(id)
- Returns nil (no parent linkage).

## Security

Delete must reject any id that is empty, `.`, `..`, or contains `/` or `\`, and
must match the UUID pattern before removal — this prevents deleting the whole
`brain/` directory or escaping it.

## Tests (fixture temp brain dir)

- List finds UUID conversation dirs, sets UpdatedAt from transcript mtime,
  leaves Title/CWD empty, ignores non-UUID dirs.
- UserPrompts extracts `USER_INPUT` text and ignores `PLANNER_RESPONSE`.
- ValidateResume checks binary on PATH.
- Delete removes a conversation dir; rejects bad ids (`..`, empty, slashes) and
  never removes the brain root.
- Capabilities match the table above.
