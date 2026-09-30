# Spec 009 — Aggregate (Multi-Provider) View

Status: Implemented

## Purpose

Let `clisesh` discover **all** supported agent CLIs on the machine and present
their sessions in a single merged list, rather than one provider at a time.
The aggregate is itself a `domain.SessionProvider`, so the TUI and CLI treat
"all providers" exactly like a single provider.

## Selection

- Config `provider = "all"` (the **default**) enables aggregation.
- `provider = "<name>"` restricts to a single provider.
- Same override precedence as any config value (flag > env > file > default).

## Discovery

`provider.All()` constructs every registered provider at its default location.
A provider that fails to construct or whose session directory is absent simply
contributes no sessions (its `List` returns empty) — it never breaks the view.

## Behavior

- **List**: concatenates each child's `List`, then sorts newest-first across
  all providers.
- **Routing**: `UserPrompts`, `Resume`, `ValidateResume`, `Children`, and `Delete` are routed to
  the provider that owns each session (matched by `Session.ProviderID`, found
  by scanning children's lists — session IDs are globally unique).
- **Delete** groups ids by owning provider and delegates each group, so
  provider-specific behavior (e.g. Kiro's subagent cascade) is preserved.
- **Capabilities**: the union of child capabilities. Per-session feasibility is
  enforced at routing time (an unsupported action on a given session is a safe
  no-op / `ErrNotSupported`).

## Identifying a session's source

Every `Session` carries `ProviderID`. The TUI renders a standardized 3-character
brand badge (`KIR`, `AGY`, `CLD`, `CDX`, `OPC`) in each CLI's signature color.
The info dialog displays the full provider name.

## Caching

The aggregate uses a single cache file (`all-descriptions.json`) keyed by
session ID.

## Tests

- List merges and sorts newest-first across multiple providers.
- Capabilities are the union.
- UserPrompts / Resume / ValidateResume / Delete route to the correct owning provider.
- Delete groups by provider and preserves per-provider handling.
- Unknown id is a safe no-op (`ErrNotSupported` for resume, empty for delete).
