# Spec 011 — OpenAI Codex Provider

Status: Implemented

## Purpose

Implement `domain.SessionProvider` for OpenAI's Codex CLI (`codex`).

## Storage layout

Base dir: `~/.codex/` (overridable via environment variable `CODEX_HOME` or `New(baseDir)`).

```
~/.codex/
  sessions/
    YYYY/
      MM/
        DD/
          rollout-<timestamp>-<uuid>.jsonl   # Rollout transcript file
```

Sessions are partitioned by year, month, and day subdirectories.

## Capabilities

```go
HasTitle:        false   // Codex does not store a separate session title
HasParent:       false
CanResume:       true    // codex resume <id>
CanDelete:       true    // removes the rollout .jsonl file and prunes empty date dirs
CascadeDelete:   false
ProvidesPrompts: true    // user prompt entries in the rollout
```

## Behavior

### List
- Walk `sessions/**/*.jsonl`.
- Extract session UUID from filename pattern.
- `UpdatedAt` is derived from file modification time and timestamp prefixes.
- `Locked` checks active process table for running Codex instances associated with this session.
- Returns sessions sorted newest-first.

### UserPrompts(id, max)
- Reads the matched `rollout-*.jsonl` file.
- Extracts user prompts where `role == "user"` or `type == "user"`.
- Returns up to `max` prompts in order.

### Resume(id)
- Replaces process with `codex resume <id>`.

### ValidateResume(id)
- Verifies `codex` is available on `PATH` using `exec.LookPath("codex")`.

### Delete(ids)
- Deletes the corresponding rollout `.jsonl` files.
- Automatically cleans up and prunes empty date subdirectories (`DD`, `MM`, `YYYY`) if no other sessions remain in them.
- Returns list of deleted IDs.
