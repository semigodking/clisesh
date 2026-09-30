# Spec 012 — OpenCode Provider

Status: Implemented

## Purpose

Implement `domain.SessionProvider` for the OpenCode CLI agent (`opencode`).
OpenCode manages sessions via a local SQLite database (`opencode.db`) and an authenticated local HTTP service (`/api/session`).

## Storage & Communication

- Config directory: `~/.config/opencode/`
- Data directory: `~/.local/share/opencode/`
- Service endpoint: `http://127.0.0.1:<port>/api/session`
- Database: `opencode.db`

OpenCode sessions use IDs prefixed with `ses_` (e.g. `ses_f14aac24bffeJaXyEHtxaBzxOU`).

## Capabilities

```go
HasTitle:        true    // OpenCode provides native session titles
HasParent:       true    // OpenCode subagent parent/child relationships
CanResume:       true    // opencode --session <id>
CanDelete:       true    // cascades or uses opencode session delete
CascadeDelete:   true
ProvidesPrompts: true    // extracts user messages via API or DB
```

## Behavior

### List
- Queries local service endpoint `GET /api/session` or reads `opencode.db`.
- Retrieves session metadata: ID, title, CWD, updated timestamp, and parent ID.
- Checks active sessions via `/api/session/active` to set `Locked = true`.
- In TUI list rendering, the `ses_` prefix is trimmed from `ShortID()` for a cleaner interface.
- Returns sessions sorted newest-first.

### UserPrompts(id, max)
- Fetches transcript messages via `GET /api/session/:id/message` or fallback `opencode session export <id>`.
- Extracts user turn messages up to `max`.

### Resume(id)
- Replaces process with `opencode --session <id>`.

### ValidateResume(id)
- Checks whether `opencode` binary exists on system `PATH`.

### Children(id)
- Returns IDs of sessions whose `parent_id == id`.

### Delete(ids)
- Deletes session entries via service API or CLI command `opencode session delete <id>`.
- Cascades deletion to child sessions if `CascadeDelete == true`.
