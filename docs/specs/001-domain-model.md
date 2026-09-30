# Spec 001 — Domain Model

Status: Implemented (Task 1)

## Purpose

Define the provider-neutral core types every other package depends on:
`Session`, `Capabilities`, and the `SessionProvider` interface. The `domain`
package must have **no dependencies** on other internal packages.

## `Session`

| Field        | Type             | Meaning                                                        |
|--------------|------------------|----------------------------------------------------------------|
| `ID`         | `string`         | Provider-native unique id (usually UUID).                      |
| `ProviderID` | `string`         | Owning provider (e.g. `kiro`, `antigravity`, `claude`, `codex`, `opencode`). |
| `Title`      | `string`         | Session title if the provider records one; may be empty.       |
| `Description`| `string`         | Cached/generated summary for display (set by the app).         |
| `CWD`        | `string`         | Working directory, if known.                                   |
| `UpdatedAt`  | `time.Time`      | Last-modified time; used for newest-first sorting.             |
| `Locked`     | `bool`           | Session is active/live and must not be bulk-deleted.           |
| `ParentID`   | `string`         | Parent session id for subagents; empty otherwise.              |
| `Raw`        | `map[string]any` | Provider-specific metadata (debug/advanced).                   |

Helpers:
- `IsSubagent() bool` — true when `ParentID != ""`.
- `ShortID() string` — first 8 chars of `ID` (or whole id if shorter, e.g. stripping OpenCode `ses_` prefix in display).

## `Capabilities`

Boolean flags advertised by each provider: `HasTitle`, `HasParent`,
`CanResume`, `CanDelete`, `CascadeDelete`, `ProvidesPrompts`. New flags added
later default to `false` so existing providers keep conservative behavior.

## `SessionProvider` interface

```go
ID() string
Capabilities() Capabilities
List(ctx context.Context) ([]Session, error)
UserPrompts(ctx context.Context, id string, max int) ([]string, error)
Resume(ctx context.Context, id string) error
ValidateResume(ctx context.Context, id string) error
Delete(ctx context.Context, ids []string) (deleted []string, err error)
Children(ctx context.Context, id string) ([]string, error)
```

Rules:
- `List` returns user-facing sessions newest-first and excludes sessions the
  UI must never show (e.g. subagents when `HasParent`), unless configured.
- `ValidateResume` checks whether a session can be resumed (e.g. required CLI
  executable exists on PATH) without replacing the process.
- Optional operations return `domain.ErrNotSupported` when the corresponding
  capability is false, even though the UI is expected to check capabilities
  first.
- `Children` returns `nil` (no error) when `HasParent` is false.

## Related utilities (`internal/textutil`)

Pure display helpers used by the UI, tested independently:
`CharWidth`, `DisplayWidth`, `Truncate`, `PadToWidth`, `WrapByWidth`
(CJK-aware), `FormatAge`, and `SingleLine`.

## Tests

- `ShortID`, `IsSubagent`, newest-first sort ordering.
- `DisplayWidth`/`Truncate`/`PadToWidth`/`WrapByWidth` for ASCII and CJK.
- `FormatAge` across second/minute/hour/day/month/year boundaries.
- `SingleLine` string sanitization.
