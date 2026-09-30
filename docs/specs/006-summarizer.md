# Spec 006 — Summarizer

Status: Implemented (Tasks 6 & 7)

## Purpose

Generate short human-readable descriptions for sessions. Summarization is
pluggable via a `Summarizer` interface with two primary backends: local agent CLI
subprocesses and OpenAI-compatible HTTP APIs.

## Interface

```go
type Item struct {
    ID      string   // session id
    Title   string   // session title (may be empty)
    Prompts []string // user prompts, in order
}

type Summarizer interface {
    // Summarize returns id -> description for the given items.
    Summarize(ctx context.Context, items []Item) (map[string]string, error)
}
```

## Description strategy (shared, backend-independent)

For each item, `DescribeOrPrompt` decides:
1. If the item is **empty** (see below), the description is the sentinel
   `"(empty session)"` and **no LLM call is made** — regardless of any title
   (decision 1a).
2. Else, if `Title` is non-empty and its display width ≤ `TitleLen`, use the
   title directly (no LLM call).
3. Else, the item needs LLM summarization from its concatenated prompts.
4. Residual safety: if there are no prompts and no usable title, the
   description is `"(no description)"` (rarely reached now that empty sessions
   are caught by step 1).

Items needing the LLM are processed via the concurrent worker pool (`batch_size`).
`RunBatched` short-circuits empty items to `"(empty session)"` before batching,
so empties are never included in an LLM request.

### Empty sessions (decision 1a)

A session is **empty** when it has **no user prompts at all** — `IsEmpty`
returns true when the normalized, joined prompts are the empty string (nil,
absent, all-blank, or whitespace-only prompts all count as empty). Emptiness is
determined **solely by prompts**, independent of the title. Empty sessions get
the `EmptySession` sentinel `"(empty session)"` and are never summarized.

### Empty → non-empty re-check on startup (decision 2a)

Cached descriptions are consulted at startup. A **non-empty** cached
description is trusted and reused as-is (no prompt fetch). A cached description
equal to the `EmptySession` sentinel is **re-checked on every startup**: the
session's prompts are fetched again, and

- if the session now has prompts, it is scheduled for summarization; or
- if it is still empty, the `"(empty session)"` sentinel is kept (and persisted
  so it stays stable).

Because re-summarization forces prompt-based summarization through the same
`RunBatched` path, pressing `r` on a session that is still empty leaves its
description as `"(empty session)"`.

## Smart Prompt Extraction & Code Scrubbing

To keep prompt sizes compact, reduce noise, and optimize LLM token usage:
1. **Head + Tail Selection**: Only the initial `head_prompts` (default: 2) and latest `tail_prompts` (default: 1) are extracted from long conversations, capturing task goals and recent progress while skipping lengthy back-and-forth debugging in the middle.
2. **Code Noise Scrubbing**: Fenced code blocks (```` ```...``` ````) within prompts are replaced with compact placeholders (`[code]`).
3. **Character Caps**: Individual prompts are truncated to `prompt_max_chars` (default: 400), and total combined session text is capped to `max_chars` (default: 1500).

## Worker Pool & UI Streaming

In the TUI:
- Background summarization runs in a concurrent worker pool of size `batch_size` (default: 5).
- Up to `batch_size` items are actively summarized (`Summarizing…`) while remaining items wait in queue (`Queued for summary…`).
- **1-by-1 Streaming Updates**: As soon as any individual session finishes, its row updates immediately in the UI and cache, and the next queued session begins processing.
- **Failure State Preservation**: If summarization fails, sessions revert to their exact original pre-summarize descriptions (precedence: snapshot > cache > title > NoDescription), avoiding loss of valid existing summaries.

## Rate Limiting (RPM Control)

HTTP LLM backends strictly enforce requests-per-minute limits via `llm.Limiter`:
- Uses a rolling 60-second sliding window tracking exact timestamps of recent outgoing calls.
- When the request count reaches `rpm`, subsequent requests block until older timestamps roll out of the window.
- Respects `context.Context` cancellation so aborted operations do not hang.
- Configurable globally (`summarizer.rpm`), per-backend (`summarizer.llm.<name>.rpm`), or via CLI flag `--rpm`.

## Subprocess backend

Shells out to a configurable local agent CLI in non-interactive mode. Multiple
named subprocess backends can be defined via `[summarizer.subprocess.<name>]`:

```toml
[summarizer.subprocess.kiro]
command = "kiro-cli"
args = ["chat", "--no-interactive", "--model", "{model}", "--effort", "low", "--wrap", "never", "{prompt}"]

[summarizer.subprocess.agy]
command = "agy"
args = ["--print", "{prompt}"]
```

Arg templates support two tokens:
- `{model}` → the configured summarizer model (or global model).
- `{prompt}` → the prompt text to summarize.

If no argument contains `{prompt}`, the prompt is appended as the final
argument. Output is ANSI-stripped before parsing. Timeout comes from config
(`summarizer.timeout_sec`).

## HTTP / LLM backend

Configured under `[summarizer.llm.<name>]`. POSTs to `<base_url>/chat/completions`.
Supports different message formats via `format`:
- `format = "openai"` (default): single user message.
- `format = "system_user"`: splits system instructions and session items.

Headers: `Authorization: Bearer <api_key>` (omitted if empty),
`Content-Type: application/json`, plus any custom headers defined under `headers`.
The response's `choices[0].message.content` is parsed with the shared parser.
The API key is never logged; error messages redact it.

## Tests

- `IsEmpty`: nil, `[""]`, and whitespace-only prompts → true; real content → false.
- `DescribeOrPrompt`: empty prompts → `"(empty session)"`; title-fits with prompts → title; long title with content → needs LLM; CJK title width respected.
- `RunBatched`: mixed empty + real items map empties to `"(empty session)"` and invokes LLM only for real items.
- Rate limiter (`TestLimiterBlocksWhenLimitExceeded`, `TestLimiterContextCanceled`, `TestLimiterConcurrentReal`).
- Smart prompt extraction and code block scrubbing.
- Startup classification (`classifyDescriptions`).
- Response parser: numbered lists with `:`/`.`, blockquote/quote stripping.
- Subprocess backend via injected fake command (success + timeout).
- OpenAI backend via `httptest.Server`: request shape, response parsing, HTTP error, key redaction.

## Demo

`clisesh summarize --provider kiro --backend subprocess <ids...>` prints
`id: description` and writes them to the provider cache.
