# Spec 008 — Antigravity Provider

Status: Implemented (Task 10)

## Purpose

Implement `domain.SessionProvider` for Google's Antigravity CLI (`agy`). This
provider deliberately differs from Kiro (directory-per-conversation, no title,
different resume/delete) to prove the abstraction supports new CLIs without
changing domain, tui, summarize, or cache.

## Storage layout

Base dir: `~/.gemini/antigravity-cli/brain/` (overridable via `New(baseDir)`).

Antigravity CLI uses a hybrid layout:
1. **Brain directory**: `~/.gemini/antigravity-cli/brain/<uuid>/`
   ```
   brain/<uuid>/.system_generated/logs/transcript.jsonl
   ```
   Contains plaintext JSON Lines transcripts with `USER_INPUT` entries for prompt extraction.
2. **Conversations database**: `~/.gemini/antigravity-cli/conversations/<uuid>.db`
   SQLite database holding `executor_metadata` containing the session's workspace path (`CWD`).

Timestamps come from the newest mtime across the transcript, brain directory, and database file.

Transcript entries (one JSON object per line) include:
- `USER_INPUT` — the user's messages.
- `PLANNER_RESPONSE` — the assistant's messages.

## Capabilities

```
HasTitle:        false   // no title recorded
HasParent:       false   // no subagent linkage
CanResume:       true    // agy --conversation <id>
CanDelete:       true    // remove conversation directory and db file
CascadeDelete:   false
ProvidesPrompts: true    // USER_INPUT entries
```

## Behavior

### List
- Read entries under `brain/` and `conversations/` (`<uuid>.db`).
- `UpdatedAt` = newest mtime among transcript file, brain dir, and database file.
- `CWD` = extracted from `conversations/<uuid>.db` (via workspace permission pattern).
- `Locked` = whether `presence/<id>.lock` is actively locked via flock by a running `agy` process.
- `Title` is empty; the app relies on summarization.
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
- Remove `brain/<id>/` directory.
- Remove `conversations/<id>.db` file.
- Remove lingering `presence/<id>.lock` file.

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
