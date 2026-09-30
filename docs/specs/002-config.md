# Spec 002 — Configuration

Status: Implemented (Task 2)

## Purpose

Provide layered configuration with clear precedence and an XDG-aware file
location, so users can set persistent preferences and override them per run.

## Precedence (highest wins)

1. **CLI flags** (e.g. `--provider`, `--theme`, `--model`, `--rpm`)
2. **Environment variables** prefixed `CLISESH_`
3. **TOML file** at `$XDG_CONFIG_HOME/clisesh/config.toml`
   (default `~/.config/clisesh/config.toml`)
4. **Built-in defaults**

## File location

- If `$XDG_CONFIG_HOME` is set: `$XDG_CONFIG_HOME/clisesh/config.toml`.
- Else: `~/.config/clisesh/config.toml`.
- Overridable with `--config <path>` / `CLISESH_CONFIG`.
- A missing file is **not** an error; defaults (plus env/flags) are used.

## Schema

```toml
provider  = "all"          # active provider: "all" | "kiro" | "antigravity" | "claude" | "codex" | "opencode"
theme     = "default"      # theme name
title_len = 80             # max description length used by summarizer/UI
cache_dir = ""             # override cache dir (default ~/.cache/clisesh)

[summarizer]
backend          = "subprocess.kiro" # e.g. "subprocess.kiro", "subprocess.agy", "llm.deepseek", "llm.glm"
model            = "qwen3-coder-next"
title_len        = 80           # optional per-summarizer override
timeout_sec      = 60
batch_size       = 5            # worker pool concurrency & chunk capacity (default: 5)
rpm              = 30           # max API requests per minute (0 = unlimited)
max_prompts      = 10           # max prompts extracted per session (default: 10)
head_prompts     = 2            # initial prompts to capture task goal (default: 2)
tail_prompts     = 1            # latest prompts to capture task outcome (default: 1)
prompt_max_chars = 400          # character limit per individual prompt (default: 400)
max_chars        = 1500         # character limit per session in combined prompt (default: 1500)

[summarizer.subprocess.kiro]
command = "kiro-cli"
args    = ["chat", "--no-interactive", "--model", "{model}", "--effort", "low", "--wrap", "never", "{prompt}"]

[summarizer.subprocess.agy]
command = "agy"
args    = ["--print", "{prompt}"]

[summarizer.llm.openai]
base_url    = "https://api.openai.com/v1"
api_key     = "${OPENAI_API_KEY}"  # literal or ${ENV_VAR} indirection
model       = "gpt-4o-mini"
temperature = 0.2
timeout_sec = 60
format      = "openai"             # "openai" or "system_user"
rpm         = 60                   # per-backend RPM override

[summarizer.llm.deepseek]
base_url    = "https://api.deepseek.com/v1"
api_key     = "${DEEPSEEK_API_KEY}"
model       = "deepseek-chat"
temperature = 0.2
format      = "openai"

[theme_overrides]                  # optional per-role color overrides (spec 003)
# status_fg = "250"
# status_bg = "237"
# footer_fg = "black"
# footer_bg = "white"
# key_fg = "white"
# key_bg = "black"
# provider_opencode_fg = "brightmagenta"
```

## Environment variables

- `CLISESH_PROVIDER`
- `CLISESH_THEME`
- `CLISESH_TITLE_LEN`
- `CLISESH_CACHE_DIR`
- `CLISESH_CONFIG`
- `CLISESH_SUMMARIZER_BACKEND`
- `CLISESH_SUMMARIZER_MODEL`
- `CLISESH_SUMMARIZER_BATCH_SIZE`
- `CLISESH_SUMMARIZER_RPM`
- `CLISESH_SUMMARIZER_MAX_PROMPTS`
- `CLISESH_SUMMARIZER_HEAD_PROMPTS`
- `CLISESH_SUMMARIZER_TAIL_PROMPTS`

## Subcommands

- `clisesh config print` — show the effective configuration (secrets redacted).
- `clisesh config init` — generate a default configuration file from the
  built-in defaults (`config.Default()`).
  Pass `--force` to overwrite an existing config file without prompting.

## Secret handling

- `api_key` may be a literal or `${ENV_VAR}`. `${ENV_VAR}` is resolved at load across all `[summarizer.llm.*]` definitions.
- `Config.Redacted()` returns a copy with any non-empty API key replaced by
  `"***"` for safe printing/logging. `config print` uses the redacted form.

## Defaults

| Key                         | Default                         |
|-----------------------------|---------------------------------|
| `provider`                  | `all`                           |
| `theme`                     | `default`                       |
| `title_len`                 | `80`                            |
| `cache_dir`                 | `~/.cache/clisesh`              |
| `summarizer.backend`        | `subprocess.kiro`               |
| `summarizer.model`          | `qwen3-coder-next`              |
| `summarizer.timeout_sec`    | `60`                            |
| `summarizer.batch_size`     | `5`                             |
| `summarizer.rpm`            | `0` (unlimited)                 |
| `summarizer.max_prompts`    | `10`                            |
| `summarizer.head_prompts`   | `2`                             |
| `summarizer.tail_prompts`   | `1`                             |
| `summarizer.prompt_max_chars` | `400`                         |
| `summarizer.max_chars`      | `1500`                          |
