# clisesh

[![CI](https://github.com/semigodking/clisesh/actions/workflows/ci.yml/badge.svg)](https://github.com/semigodking/clisesh/actions/workflows/ci.yml)
[![Release](https://github.com/semigodking/clisesh/actions/workflows/release.yml/badge.svg)](https://github.com/semigodking/clisesh/actions/workflows/release.yml)
[![GitHub Release](https://img.shields.io/github/v/release/semigodking/clisesh?include_prereleases&style=flat-square)](https://github.com/semigodking/clisesh/releases)

[English](README.md) | 简体中文

`clisesh` 是一个专为开发者设计的终端 UI (TUI) 工具，用于**快速查找并恢复各类 AI Agent CLI 的历史工作环境**。如果你经常在多个不同 AI 命令行工具（如 Kiro、Antigravity、Claude Code、Codex、OpenCode 等）之间切换执行长时间任务，`clisesh` 可以将所有活跃与历史会话聚合在一个统一的界面中，利用 AI 生成的任务摘要进行快速检索，并通过单次按键无缝恢复对应的工作流。

通过充分挖掘与复用历史会话的上下文价值，`clisesh` 避免了重复初始化提示词与背景说明，在大幅提升开发效率的同时有效节省 Token 消耗。

![clisesh TUI](images/main_tui.png)

## 功能特性

- **多 Agent 会话统一聚合**：在同一个按时间倒序排列的列表中，统一聚合 **Kiro**、**Antigravity (`agy`)**、**Claude Code**、**OpenAI Codex** 和 **OpenCode** 的所有会话。
- **智能任务摘要与手动编辑**：利用本地 CLI 或云端 LLM 自动生成任务摘要；支持按 `m` 键随时呼出弹窗手动编辑并保存会话描述。
- **一键恢复工作流**：选中任何历史会话按下 `Enter` 键，即可无缝接管并恢复到目标 Agent CLI 的交互环境中。
- **活跃进程安全防护**：自动检测正在运行中的活跃会话（标记为 `●`），并在批量删除时主动防护跳过，杜绝误杀。
- **即时检索与批量操作**：支持按 `/` 键进行标题、描述和会话 ID 的实时模糊过滤，按空格键多选会话进行批量管理。

## 支持的 Provider

`clisesh` 默认自动发现并聚合本机安装的 Agent CLI 会话（`provider = "all"`）。

| Provider | ID | 恢复命令 | 运行中防护 |
| :--- | :--- | :--- | :--- |
| **Kiro** | `kiro` | `kiro-cli chat --resume-id <id>` | ✅ 自动识别 (`●`) |
| **Antigravity** | `antigravity` / `agy` | `agy --conversation <id>` | ✅ 自动识别 (`●`) |
| **Claude Code** | `claude` | `claude --resume <id>` | ✅ 自动识别 (`●`) |
| **OpenAI Codex** | `codex` | `codex resume <id>` | ✅ 自动识别 (`●`) |
| **OpenCode** | `opencode` | `opencode --session <id>` | ✅ 自动识别 (`●`) |

> 你也可以通过命令行参数 `--provider <id>` 或在配置文件中设置 `provider = "..."` 针对特定 Provider 运行。

## 安装指南

### 下载预编译二进制（推荐）

直接前往 [GitHub Releases](https://github.com/semigodking/clisesh/releases) 页面下载适合你操作系统与 CPU 架构的压缩包：
- **Linux**：`x86_64` (amd64) 或 `arm64`
- **macOS**：Apple Silicon (`arm64`) 或 Intel (`amd64`)

解压并将 `clisesh` 放入系统 `PATH` 路径即可：
```bash
tar -xzf clisesh_*.tar.gz
sudo mv clisesh /usr/local/bin/
```

### 源码编译安装

编译环境要求：Go 1.22+。

```bash
git clone https://github.com/semigodking/clisesh
cd clisesh
make build          # 在 bin/clisesh 生成静态二进制文件
```

## 快速上手与命令

```bash
clisesh                       # 启动 TUI 界面（默认聚合所有 Provider）
clisesh tui --provider kiro   # 针对指定 Provider 启动 TUI
clisesh list --provider all   # 命令行非交互式列出所有会话（方便脚本处理）
clisesh summarize [id...]     # 在后台批量生成并缓存会话描述
clisesh config init           # 生成默认配置文件模板 (~/.config/clisesh/config.toml)
clisesh version               # 查看版本号
```

### 常用快捷键

| 按键 | 功能 | 说明 |
| :--- | :--- | :--- |
| `↑` / `k`, `↓` / `j` | 移动光标 | 向上 / 向下导航 |
| `g` / `home`, `G` / `end` | 页面跳转 | 快速跳至列表顶部 / 底部 |
| `pgup` / `pgdown` | 翻页 | 快速滚动 |
| `space` (空格) | 标记/多选 | 切换多选标记，用于批量操作 |
| `enter` (回车) | 恢复会话 | 在对应的 Agent CLI 中恢复该会话 |
| `m` | 编辑描述 | 打开弹窗手动编辑会话描述并保存至缓存 |
| `r` | 重新总结 | 将选中的会话推入后台重新生成摘要 |
| `d` | 删除会话 | 确认后删除（运行中的 `●` 会话自动受保护跳过） |
| `i` | 查看详情 | 弹出弹窗查看会话 ID、创建时间、工作目录 CWD 等 |
| `e` | 查看错误 | 总结失败时查看完整错误堆栈日志 |
| `/` | 搜索过滤 | 实时过滤标题、描述与会话 ID（`esc` 清除） |
| `q` / `ctrl+c` | 退出 | 退出程序 |

## 配置说明

`clisesh` 开箱即用，零配置即可正常运行。如需深度定制，可以生成配置文件：

```bash
clisesh config init
```

配置文件路径为 `~/.config/clisesh/config.toml`（或 `$XDG_CONFIG_HOME/clisesh/config.toml`）。

### 配置示例

```toml
# 目标 Provider: "all" (默认), "kiro", "antigravity", "claude", "codex", 或 "opencode"
provider = "all"

# 色彩主题: "default", "gray", "high-contrast", 或 "256"
theme = "default"

[summarizer]
# 子进程 CLI 后端 (如 "subprocess.kiro", "subprocess.agy")
# 或 HTTP LLM 后端 (如 "llm.openai", "llm.deepseek", "llm.glm")
backend = "subprocess.kiro"
batch_size = 5                   # 后台并发总结工作线程数

[summarizer.subprocess.kiro]
command = "kiro-cli"
args = ["chat", "--no-interactive", "--model", "{model}", "--effort", "low", "--wrap", "never", "{prompt}"]

[summarizer.llm.openai]
base_url = "https://api.openai.com/v1"
api_key = "${OPENAI_API_KEY}"
model = "gpt-4o-mini"
rpm = 60                         # 每分钟最大 API 请求速率限制
```

> 完整的高级配置项（包括自定义颜色覆盖、Prompt 截取规则与各 LLM 格式）请参考 [`configs/config.example.toml`](configs/config.example.toml)。

## 相关文档与开发

- [架构设计概览](docs/DESIGN.md) — 系统分层与设计原则
- [Provider 能力矩阵与存储规范](docs/providers/capability-matrix.md) — 各 Agent CLI 存储格式与适配
- [组件规范说明](docs/specs/) — 模块级规范文档

```bash
make test     # 运行所有单元测试
make lint     # 运行代码规范检查 (go vet + gofmt)
make dist     # 交叉编译 Linux 与 macOS 全平台发布包
```

## 打赏

如果你觉得这个软件对你有帮助，欢迎打赏支持开发。

- ![微信](images/mm_reward_qrcode.png)
- [paypal.me/redsocks2](https://paypal.me/redsocks2)

## 开源协议

MIT
