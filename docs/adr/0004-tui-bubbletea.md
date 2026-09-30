# ADR 0004: Bubble Tea for the TUI

- Status: Accepted
- Date: 2026-09-24

## Context

We need a maintainable, themeable full-screen terminal UI with a scrollable
list, filter input, modal editing, and popup dialogs.

## Decision

Use the **Charm** stack: **Bubble Tea** (Elm-architecture framework),
**Lipgloss** (declarative styling), and **Bubbles** (prebuilt `list`,
`viewport`, `textinput`, `help` components).

## Rationale

- The Elm architecture (Model/Update/View) yields testable update logic.
- Lipgloss styles map directly onto our semantic theme roles, making theming
  a matter of building styles from the resolved theme.
- Bubbles components reduce boilerplate for list/filter/help/input.

## Consequences

- The `tui` package depends on Charm libraries; the rest of the app does not.
- Update logic is unit-testable without a real terminal; full rendering is
  verified manually and via test suites.
