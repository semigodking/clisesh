# Provider Capability Matrix

This matrix records how each agent CLI stores sessions and which clisesh
capabilities its provider supports. It is grounded in investigation of real
on-disk layouts (July–September 2026). Formats marked *undocumented* are
observed from disk and may change between releases — providers guard them
with shape/version checks.

## Storage formats

| Provider     | Base dir (env override)                     | Layout                                                        | Title? | Metadata source            |
|--------------|---------------------------------------------|---------------------------------------------------------------|--------|----------------------------|
| **Kiro**     | `~/.kiro/sessions/cli/`                      | `<uuid>.json` (meta) + `<uuid>.jsonl` (transcript) + `.lock`  | Yes    | `.json`: title, cwd, updated_at, parent_session_id |
| **Antigravity** (`agy`) | `~/.gemini/antigravity-cli/brain/` | `<uuid>/.system_generated/logs/transcript.jsonl` (dir per convo) | No | transcript entries only    |
| **Claude Code** (`claude`) | `~/.claude/projects/` (`CLAUDE_CONFIG_DIR`) | `<encoded-cwd>/<uuid>.jsonl` + `~/.claude/sessions/`          | No     | `.jsonl` entries: user prompts, cwd |
| **OpenAI Codex** (`codex`) | `~/.codex/sessions/` (`CODEX_HOME`)         | `YYYY/MM/DD/rollout-*.jsonl`                                  | No     | `.jsonl` entries: user prompts, cwd |
| **OpenCode** (`opencode`) | `~/.local/share/opencode/` + `~/.config/opencode/` | Service HTTP API (`/api/session`) + `opencode.db` / CLI | Yes | API / DB: title, cwd, updated_at, parent_id |

Prompt extraction:
- **Kiro**: transcript lines with `"kind":"Prompt"`.
- **Antigravity**: transcript entries of type `USER_INPUT` (assistant replies are `PLANNER_RESPONSE`).
- **Claude Code**: transcript entries of type `"user"` with `message.content`.
- **OpenAI Codex**: rollout entries with `role: "user"` or `type: "user"`.
- **OpenCode**: message entries of type `"user"` via `/api/session/:id/message` or `opencode session export`.

Active lock detection:
- **Kiro**: `<uuid>.lock` existence check in the sessions directory.
- **Antigravity**: BSD `flock` check on `~/.gemini/antigravity-cli/presence/<uuid>.lock`.
- **Claude Code**: Live PID check in `~/.claude/sessions/<pid>.json` (`kill(pid, 0)`) and `<uuid>.lock`.
- **OpenAI Codex**: passive process/timestamp check.
- **OpenCode**: active session map via `GET /api/session/active`.

Resume / validate / delete:
- **Kiro**: resume `kiro-cli chat --resume-id <id>`; validation checks `kiro-cli` binary on PATH; delete = remove session files & subagent cascade.
- **Antigravity**: resume `agy --conversation <id>`; validation checks `agy` binary on PATH; delete = `rm -rf` the conversation directory.
- **Claude Code**: resume `claude --resume <id>`; validation checks `claude` binary on PATH; delete = remove `<project>/<uuid>.jsonl`.
- **OpenAI Codex**: resume `codex resume <id>`; validation checks `codex` binary on PATH; delete = remove rollout file and prune empty parent date directories.
- **OpenCode**: resume `opencode --session <id>`; validation checks `opencode` binary on PATH; delete = `opencode session delete <id>`. In TUI display, `ses_` prefix is stripped for readability.
- **Safety guarantee**: In the TUI, active sessions (`Locked == true`) are protected from accidental deletion; attempting to delete an active session is blocked, and batch operations safely skip active targets.

## Capability flags

| Capability        | Kiro | Antigravity | Claude Code | Codex | OpenCode | Notes                                             |
|-------------------|:----:|:-----------:|:-----------:|:-----:|:--------:|---------------------------------------------------|
| `HasTitle`        |  ✅  |     ❌      |     ❌      |  ❌   |    ✅    | Kiro & OpenCode provide native session titles     |
| `HasParent`       |  ✅  |     ❌      |     ❌      |  ❌   |    ✅    | Kiro & OpenCode link parent/child subagents       |
| `CanResume`       |  ✅  |     ✅      |     ✅      |  ✅   |    ✅    | All support session resume with ValidateResume    |
| `CanDelete`       |  ✅  |     ✅      |     ✅      |  ✅   |    ✅    | All support deletion                              |
| `CascadeDelete`   |  ✅  |     ❌      |     ❌      |  ❌   |    ✅    | Kiro & OpenCode support cascade child deletion    |
| `ProvidesPrompts` |  ✅  |     ✅      |     ✅      |  ✅   |    ✅    | All expose user prompts in transcripts            |

## Future providers (Phase 2 & beyond)

| Provider     | Base dir                                     | Layout        | Title? | Notes                                  |
|--------------|----------------------------------------------|---------------|--------|----------------------------------------|
| DeepSeek     | `~/.deepseek/` or API-based                  | CLI transcripts / cache                       | No | LLM summarization supported            |
| GLM          | `~/.glm/` or API-based                       | CLI transcripts / cache                       | No | LLM summarization supported            |
| Cursor       | `~/.cursor/chats/<id>/<uuid>/`               | SQLite `store.db` + `meta.json` | meta.json | different storage model (SQLite)       |
| Aider        | `~/.aider.chat.history.md` or per-repo        | chat history markdown                         | No | per-repo or global chat logs           |

> Caution: several of these paths and formats are undocumented by the vendor
> and have changed across releases. Any provider built on them must fail
> gracefully when the shape does not match expectations.
