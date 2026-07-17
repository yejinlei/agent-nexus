# agent-nexus — AI Agent 配置自动化工具

一键自动发现、备份、配置本机所有 AI coding agent（codex / claude / kimi / deepseek / opencode / cursor 等），将它们统一接入 CCX Desktop 代理。

## 功能

- **自动发现**：扫描本机已安装的 AI agent（CLI 工具 + IDE）
- **代理检测**：自动读取 CCX Desktop 配置（URL、Key、模型映射表）
- **配置写入**：支持 `--url` / `--key` 命令行选项，也可直接输入 URL 和 Key
- **自动备份**：配置生效前自动备份原有配置文件
- **一键配置**：`agent-nexus configure` 完成完整流程
- **模型路由**：三层模型重定向机制，匹配最佳后端
- **彩色输出**：终端彩色状态显示

## 支持的 Agent

### 可配置（通过代理转发）

| Agent | 配置文件 | 类型 | 协议 |
|-------|---------|------|------|
| codex | `~/.codex/config.toml` | CLI | OpenAI-compatible |
| claude | `~/.claude/settings.json` | CLI | Anthropic |
| kimi | `~/.kimi/config.toml` | CLI | ACP |
| deepseek | `~/.deepseek/config.toml` | CLI | OpenAI-compatible |
| opencode | `~/.config/opencode/opencode.jsonc` | CLI | AI SDK |
| openclaw | `~/.openclaw/openclaw.json` | CLI | Custom |
| cursor | `Cursor/User/settings.json` | IDE | OpenAI-compatible |
| codebuddy | `~/.codebuddy/settings.json` | CLI | Anthropic (Claude Code兼容) |
| hermes | `~/.hermes/config.yaml` | CLI | ACP |
| kiro | `~/.kiro/config.yaml` | CLI | ACP |
| grok | `~/.grok/config.yaml` | CLI | ACP |
| qoder | `~/.qoder/config.yaml` | CLI | ACP |
| trae | `~/.traecli/config.yaml` | CLI | ACP |

### 不可配置（无外部模型配置字段）

| Agent | 类型 | 说明 |
|-------|------|------|
| antigravity | CLI | Google Gemini 服务，无外部模型配置字段 |
| copilot | CLI | 模型由 GitHub 账户权益决定，无外部模型配置字段 |
| deveco | CLI | 基于 OpenCode 引擎，内置华为账号认证与自有模型目录 |
| pi | CLI | Inflection AI 代理，无外部模型配置字段 |
| qoder-ide | IDE | VS Code 派生，自有 AI 后端 |
| trae-ide | IDE | VS Code 派生，自有 AI 后端 |
| codebuddy-ide | IDE | VS Code 派生，自有 AI 后端 |
| windsurf | IDE | VS Code 派生，自有 AI 后端 |
| zed | IDE | 无内置 AI Agent，依赖外部工具 |

### 暂缺配置写入器

| Agent | 类型 | 说明 |
|-------|------|------|
| lmstudio | CLI | 待实现配置写入器 |
| clawx | IDE | 待实现配置写入器 |

## 安装

### 方式一：使用编译好的可执行文件

直接下载 `agent-nexus.exe`，在终端运行：

```powershell
.\agent-nexus.exe --help
```

### 方式二：从源码编译

```powershell
go mod tidy
go build -o agent-nexus.exe
```

## 快速开始

```powershell
# 一键扫描 → 检测代理 → 备份 → 配置
agent-nexus configure
```

## 命令参考

```powershell
agent-nexus discover   扫描并列出已安装的 AI agent
agent-nexus detect     检测 CCX Desktop 代理配置（URL、Key、模型映射）
agent-nexus backup     备份所有 agent 配置文件
agent-nexus configure  备份后一键自动配置所有可配置的 agent
agent-nexus status     显示各 agent 当前配置状态
agent-nexus route      显示模型路由表
```

## --url / --key 选项

支持直接通过命令行传入代理 URL 和 API Key，无需依赖 CCX Desktop 自动嗅探：

```powershell
agent-nexus configure --url http://127.0.0.1:8080/v1 --key sk-xxx
agent-nexus detect --url http://proxy:9000/v1 --key abc
agent-nexus route --url http://proxy:9000/v1 --key abc
```

不传参数时仍使用自动嗅探（原有行为不变）。

## 模型路由（三层机制）

**第一层：CCX Desktop 自动映射** — Agent 传入模型名（如 `gpt-5.5`），CCX 自动映射到实际后端模型

**第二层：写入器默认模型**

| Agent | 写入模型 | → 实际后端 |
|-------|---------|-----------|
| codex | gpt-5.5 | sensenova-6.7-flash-lite |
| claude | fable | glm-5.2 |
| kimi | ccx/gpt-5.5 | sensenova-6.7-flash-lite |
| deepseek | sensenova-6.7-flash-lite | sensenova-6.7-flash-lite |
| opencode | myccx/glm-5.2 | glm-5.2 |
| cursor | sensenova-6.7-flash-lite | sensenova-6.7-flash-lite |
| codebuddy | fable | glm-5.2 |
| hermes | sensenova-6.7-flash-lite | sensenova-6.7-flash-lite |
| kiro | sensenova-6.7-flash-lite | sensenova-6.7-flash-lite |
| grok | sensenova-6.7-flash-lite | sensenova-6.7-flash-lite |
| qoder | sensenova-6.7-flash-lite | sensenova-6.7-flash-lite |
| trae | sensenova-6.7-flash-lite | sensenova-6.7-flash-lite |

**第三层：DeepSeek CLI 备选直连** — 配置中保留 sensenova 直连方案（注释形式）

## 备份与恢复

备份自动存储于：

```
~/.codex/backups/agent-configs-YYYY-MM-DD_HH-MM-SS/
```

回滚时，将备份目录中的配置文件覆盖回原位即可。

## 项目结构

```
agent-nexus/
├── main.go                          # 入口
├── go.mod
├── go.sum
├── cmd/
│   └── root.go                      # Cobra CLI 命令定义
├── internal/
│   ├── agent/                       # 各 agent 配置写入器（可插拔）
│   │   ├── agent.go                 # 接口 + 注册表
│   │   ├── codex.go
│   │   ├── claude.go
│   │   ├── kimi.go
│   │   ├── deepseek.go
│   │   ├── opencode.go
│   │   ├── openclaw.go
│   │   ├── cursor.go
│   │   ├── codebuddy.go             # [新增] 腾讯 CodeBuddy CLI
│   │   ├── hermes.go                # [新增] Hermes (Nous Research)
│   │   ├── kiro.go                  # [新增] Kiro CLI (Amazon)
│   │   ├── grok.go                  # [新增] Grok (xAI)
│   │   ├── qoder_cli.go             # [新增] Qoder CLI (阿里)
│   │   └── trae_cli.go              # [新增] Trae CLI (字节跳动)
│   ├── backup/
│   │   └── backup.go                # 备份逻辑
│   ├── discover/
│   │   └── discover.go              # 自动发现 agent
│   ├── model/
│   │   └── model.go                 # 模型路由表
│   └── proxy/
│       └── proxy.go                 # CCX Desktop 代理检测
└── README.md
```

## 扩展新 Agent

实现 `agent.ConfigWriter` 接口并注册到 `WriterRegistry` 即可：

```go
type myAgentWriter struct{}

func newMyAgentWriter() *myAgentWriter { return &myAgentWriter{} }

func (w *myAgentWriter) Name() string     { return "myagent" }
func (w *myAgentWriter) Category() string { return "cli" }
func (w *myAgentWriter) CanConfigure(p *proxy.Proxy) bool { return true }
func (w *myAgentWriter) Configure(path string, p *proxy.Proxy) error { /* 写入逻辑 */ }
func (w *myAgentWriter) Status(path string) (bool, string) { /* 状态检测 */ }
```

然后在 `agent.go` 的 `NewWriterRegistry()` 中注册：

```go
writers: []ConfigWriter{
    // ... 现有写入器
    newMyAgentWriter(),
},
```

## 注意事项

- CCX Desktop 需保持运行（监听 `127.0.0.1:3688`）
- Cursor 的字段名取决于版本，不匹配时需通过 Cursor 设置 UI 手动填入
- 敏感信息（API Key）仅写入各 agent 自身配置文件，未扩散
- 配置生效前所有原始配置文件均已备份

## License

MIT
