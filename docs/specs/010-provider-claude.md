# Spec 010 — Claude Code Provider

Status: Implemented

## Purpose

Implement `domain.SessionProvider` for Anthropic's Claude Code CLI (`claude`).

## Storage layout

Base dir: `~/.claude/` (overridable via environment variable `CLAUDE_CONFIG_DIR` or `New(baseDir)`).

```
~/.claude/
  projects/
    <encoded-cwd>/
      <uuid>.jsonl         # Transcript file per conversation
  sessions/
    <pid>.json            # Active session metadata referencing active process PID
```

The `<encoded-cwd>` directory encodes the project path by replacing path separators (`/`) with `%2F` (or underscores depending on OS/version).

## Capabilities

```go
HasTitle:        false   // Claude Code does not store a separate session title
HasParent:       false   // Subagent hierarchy is not stored in separate session files
CanResume:       true    // claude --resume <id>
CanDelete:       true    // removes the session .jsonl file
CascadeDelete:   false
ProvidesPrompts: true    // user prompt entries in the transcript
```

## Behavior

### List
- Walk `projects/*/*.jsonl`.
- The filename stem is validated against a standard UUID regex.
- `UpdatedAt` is derived from the `.jsonl` modification time.
- `Locked` is determined by scanning `~/.claude/sessions/` for active PID files pointing to the session ID and checking if the PID is alive (`syscall.Kill(pid, 0)`).
- Session `CWD` is decoded from the parent directory name or read from the transcript.
- Returns sessions sorted newest-first.

### UserPrompts(id, max)
- Scans `<project>/<id>.jsonl` line by line.
- Parses JSON entries and extracts text where `type == "user"` or `role == "user"`.
- Returns up to `max` prompts in chronological order.

### Resume(id)
- Replaces current process with `claude --resume <id>`.

### ValidateResume(id)
- Verifies that `claude` exists in system `PATH` via `exec.LookPath("claude")`.
- Returns a descriptive error with installation instructions if missing.

### Delete(ids)
- Validates each ID format.
- Removes `<encoded-cwd>/<id>.jsonl` from disk.
- Returns list of deleted IDs.
