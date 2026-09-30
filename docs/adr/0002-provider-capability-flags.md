# ADR 0002: Provider abstraction with capability flags

- Status: Accepted
- Date: 2026-09-24

## Context

Agent CLIs store sessions in very different ways (per-session JSON, dated
JSONL rollouts, SQLite, directory-per-conversation, server-backed). Some
record a title; some do not. Some link subagents; some do not. Resume and
delete mechanisms differ per tool. See the capability matrix for details.

## Decision

Define a single `SessionProvider` interface returning a normalized
`domain.Session`, and have each provider advertise a `Capabilities` struct
(`HasTitle`, `HasParent`, `CanResume`, `CanDelete`, `CascadeDelete`,
`ProvidesPrompts`). The UI reads capabilities to enable/hide actions.

## Rationale

- Avoids forcing providers to fake features they cannot support.
- The UI degrades gracefully instead of erroring on unsupported operations.
- Adding a provider is purely additive — no changes to UI/config/theme/cache.

## Consequences

- Optional interface methods must fail safely (`ErrNotSupported`) even though
  the UI is expected to check capabilities first.
- New capabilities can be added over time; providers default new flags to
  false, preserving conservative behavior.
