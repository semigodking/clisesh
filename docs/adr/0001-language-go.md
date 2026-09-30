# ADR 0001: Implement clisesh in Go

- Status: Accepted
- Date: 2026-09-24

## Context

We want a well-architected, easily distributed terminal tool that supports multiple
agent CLIs, theming, configuration, and pluggable LLM summarization.

## Decision

Build the project in **Go**.

## Rationale

- **Distribution**: a single static binary (`CGO_ENABLED=0`) with no runtime
  dependencies — much simpler than shipping interpreted environments.
- **TUI ecosystem**: Bubble Tea (Elm architecture) + Lipgloss (styling, maps
  cleanly onto our theming requirement) + Bubbles (list, viewport, textinput).
- **Concurrency**: goroutines make batched/parallel summarization simple.
- **Static typing & interfaces**: a natural fit for the provider abstraction.

## Alternatives considered

- **Interpreted scripts**: hard to distribute without bundling runtimes, and weaker concurrency.
- **Rust (ratatui)**: excellent binary/perf but steeper curve and slower initial iteration speed.

## Consequences

- Standalone, zero-dependency static executable.
- `x/text` pinned to a version compatible with the Go toolchain.
