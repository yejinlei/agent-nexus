# agent-nexus 用户使用手册

## 功能概 

- **自动发现**：扫描本机已安装?AI agent（CLI 工具 + IDE?
- **代理检?*：自动读?CCX Desktop 配置（URL、Key、模型映射表），也支持任意自定义代理
- **配置写入**：支?`--url` / `--key` 全局选项，也可直接输?URL ?Key
- **自动备份**：配置生效前自动创建版本化快?
- **一键配?*：`agent-nexus conf bak` 完成完整流程
- **模型路由**：三层模型重定向机制，匹配最佳后?
- **版本化管?*：配置快 （snapshot）、分支（branch）、差异对比（diff）、回滚（restore?
- **LLM 嗅探**：自动检?LLM 提供商的消息格式和可用模?
- **彩色输出**：终端彩色状态显?

---

## 支持?Agent

### 可配置（通过代理转发?

| Agent | 配置文件 | 类型 | 协议 |
|-------|---------|------|------|
| codex | `~/.codex/config.toml` | CLI | OpenAI-compatible |
| claude | `~/.claude/settings.json` | CLI | Anthropic |
| kimi | `~/.kimi/config.toml` | CLI | ACP |
| deepseek | `~/.deepseek/config.toml` | CLI | OpenAI-compatible |
| opencode | `~/.config/opencode/opencode.jsonc` | CLI | AI SDK |
| openclaw | `~/.openclaw/openclaw.json` | CLI | Custom |
| openclaude | `~/.openclaude-env` | CLI | OpenAI-compatible (.env) |
| cursor | `Cursor/User/settings.json` | IDE | OpenAI-compatible |
| codebuddy | `~/.codebuddy/settings.json` | CLI | Anthropic (Claude Code 兼容) |
| hermes | `~/.hermes/config.yaml` | CLI | ACP |
| kiro | `~/.kiro/config.yaml` | CLI | ACP |
| grok | `~/.grok/config.yaml` | CLI | ACP |
| qoder | `~/.qoder/config.yaml` | CLI | ACP |
| trae | `~/.traecli/config.yaml` | CLI | ACP |

### 不可配置（无外部模型配置字段?

| Agent | 类型 | 说明 |
|-------|------|------|
| antigravity | CLI | Google Gemini 服务，无外部模型配置字段 |
| copilot | CLI | 模型?GitHub 账户权益决定，无外部模型配置字段 |
| devise | CLI | 基于 OpenCode 引擎，内置华为账号认证与自有模型目录 |
| pi | CLI | Inflection AI 代理，无外部模型配置字段 |
| qoder-ide | IDE | VS Code 派生，自?AI 后端 |
| trae-ide | IDE | VS Code 派生，自?AI 后端 |
| codebuddy-ide | IDE | VS Code 派生，自?AI 后端 |
| windsurf | IDE | VS Code 派生，自?AI 后端 |
| zed | IDE | 无内?AI Agent，依赖外部工?|

---

## 安装

### 方式一：使用编译好的可 行文件

直接下载 `agent-nexus.exe`，在终端运行?

```powershell
.gent-nexus.exe --help
```

### 方式二：从源码编?

```powershell
go mod tidy
go build -o agent-nexus.exe
```

---

## 快速开 ?

```powershell
# 一键扫??检测代??创建快  ?配置所有已安装?agent
agent-nexus conf bak
```

---

## 代理支持

agent-nexus 支持两 代理接入方式?

### CCX Desktop（自动检测）

自动读取 CCX Desktop 的配置文件（`~\AppData\Roaming\ccx-desktop\.config\config.json`）和 `.env` 文件，获取代理地址、Key 和模型映射表。CCX Desktop 需保持运行（默认监?`127.0.0.1:3688`）?

### CC-Switch（自动检测）

自动读取 CC-Switch 的配置文件（`~\AppData\Roaming\cc-switch\.config\config.json`）和 `.env` 文件，获取代理地址、Key 和模型映射表。CC-Switch 需保持运行（默认监?`127.0.0.1:3688`）。检测顺序：CCX Desktop ?CC-Switch ?回退?

### 自定义代理（手动指定?

```powershell
agent-nexus conf bak --url http://127.0.0.1:8080/v1 --key sk-your-key
agent-nexus proxy detect --url https://proxy.example.com/v1 --key abc123
agent-nexus proxy route --url http://my-local-proxy:9000/v1 --key mykey
agent-nexus proxy sniff -u https://token.sensenova.cn/v1 -k sk-xxx
```

`--url` ?`--key` 是全局选项，可覆盖自动检测，支持任意代理地址和密钥。`sniff` 命令还可自动检测自定义 LLM endpoint 的消息格式和可用模型列表?

### 代理类型

| 代理类型 | 说明 |
|---------|------|
| CCX Desktop | 自动检?CCX Desktop 配置（`~\AppData\Roaming\ccx-desktop`?|
| CC-Switch | 自动检?CC-Switch 配置（`~\AppData\Roaming\cc-switch`?|
| 自定义代?| 通过 `--url` + `--key` 手动指定任意代理地址 |
| 本地代理 | 通过 `--url` 指定本地运行的代理（?`http://127.0.0.1:8080/v1`?|

> ⚠️ **每次配置仅支持一个代?*。agent-nexus 不支持同时配置多个代理，所?agent 共享同一个代理地址?

---

## 命令参?

```
agent-nexus agent install <name>            安装 agent 运行?
agent-nexus agent list                显示可安装的 agent 列表
agent-nexus agent update <name>       更新指定 agent
agent-nexus agent uninstall <name>          卸载 agent 运行?
agent-nexus agent discover [-v]             扫描已安装的 agent?v 显示模型详情?
agent-nexus proxy detect                    检?AI 代理配置
agent-nexus proxy route                     显示模型路由?
agent-nexus proxy sniff -u <url> -k <key>   嗅探 LLM 提供商的消息格式和可用模?
agent-nexus proxy db add -u <url> -k <key>   嗅探并保存到数据库（SQLite）
agent-nexus proxy db list                   列出已保存的代理配置
agent-nexus proxy db rm <id>              删除指定代理配置
agent-nexus proxy db query [filter]       查询代理配置（可选按 ID 或 URL 过滤）
agent-nexus conf bak [-b <branch>] [-m <msg>]  备份所有配置（创建快 ?
agent-nexus conf history                     列出所有配置快?
agent-nexus conf show                        创建配置快 
agent-nexus conf rollback -s <id>           恢复到指定快 （支持 "latest"?
agent-nexus conf diff --old <id> --new <id>  对比两个快 的差?
```

### 全局选项

`--url` ?`--key` 是全局选项，可用于所有命令，跳过自动嗅探直接指定代理地址和密钥：

```powershell
agent-nexus conf bak --url http://127.0.0.1:8080/v1 --key sk-xxx
agent-nexus proxy detect --url http://proxy:9000/v1 --key abc
agent-nexus proxy route --url http://proxy:9000/v1 --key abc
agent-nexus proxy sniff -u https://token.sensenova.cn/v1 -k sk-xxx
```

---

## 模型路由（三层机制）

```mermaid
flowchart LR
    A["Agent 传入<br/>模型?] --> B["第一?br/>CCX Desktop 自动映射"]
    B --> C["第二?br/>写入器默认模?]
    C --> D["第三?br/>DeepSeek CLI 直连<br/>（注释保留）"]
    D --> E["实际后端<br/>sensenova / glm"]
```

**第一层：CCX Desktop 自动映射** ?Agent 传入模型名（?`gpt-5.5`），CCX 自动映射到实际后端模?

**第二层：写入器默认模?* ?agent-nexus 写入?agent 配置文件时使用的默认模型?

| Agent | 写入模型 | ?实际后端 | 来源 |
|-------|---------|-----------|------|
| codex | gpt-5.5 | sensenova-6.7-flash-lite | CCX 映射 |
| claude | fable | glm-5.2 | CCX 映射 |
| kimi | gpt-5.5 | sensenova-6.7-flash-lite | CCX 映射 |
| deepseek | sensenova-6.7-flash-lite | sensenova-6.7-flash-lite | 直连 |
| opencode | myccx/glm-5.2 | glm-5.2 | CCX 映射 |
| cursor | sensenova-6.7-flash-lite | sensenova-6.7-flash-lite | 直连 |
| openclaw | sensenova-6.7-flash-lite | sensenova-6.7-flash-lite | CCX 映射 |
| openclaude | sensenova-6.7-flash-lite | sensenova-6.7-flash-lite | CCX 映射 |
| codebuddy | fable | glm-5.2 | CCX 映射 |
| hermes | sensenova-6.7-flash-lite | sensenova-6.7-flash-lite | CCX 映射 |
| kiro | sensenova-6.7-flash-lite | sensenova-6.7-flash-lite | CCX 映射 |
| grok | sensenova-6.7-flash-lite | sensenova-6.7-flash-lite | CCX 映射 |
| qoder | sensenova-6.7-flash-lite | sensenova-6.7-flash-lite | CCX 映射 |
| trae | sensenova-6.7-flash-lite | sensenova-6.7-flash-lite | CCX 映射 |

**第三层：DeepSeek CLI 备选直?* ?配置中保?sensenova 直连方案（注释形式）

### 模型来源说明

所有可配置 agent 均通过 OpenAI 兼容协议接入，支持自定义模型名。用户可通过 CCX Desktop 的模型重定义（model redefinition）将自定义模型名映射到后端实际模型?

---

## 配置快 与版本化管理

agent-nexus 引入类似 Git 的配置版本管理系统，支持快 、分支、差异对比和回滚?

```mermaid
graph TD
    S1["快  1<br/>(main)"] --> S2["快  2<br/>(main)"]
    S2 --> S3["快  3<br/>(main)"]
    S2 --> S4["快  4<br/>(dev)"]
    S3 --> S5["快  5<br/>(main)"]
    S5 --> |回滚| S3
```

| 命令 | 功能 |
|------|------|
| `conf show` | 创建命名快 ，类?`git commit` |
| `conf history` | 列出所有快 （版本历史），显示分支、时间、信息、文件列?|
| `conf diff --old <id> --new <id>` | 对比两个快 的差异（新增 / 删除 / 修改 / 未变?|
| `conf rollback -s <id>` | 恢复到指定快 ，支持 `latest` |
| `conf branch create <name>` | 创建新分?|
| `conf branch switch <name>` | 切换到指定分?|
| `conf branch` | 列出所有分?|
| `conf branch --show` | 显示当前分支信息 |
| `conf bak` | 兼容 格式的备份（自动版本化，默?`main` 分支?|

### 快 存储结构

```
~/.codex/backups/
├── versioning.json          # 元数据注册表（快 索?+ 分支信息?
└── snapshots/
    ├── 2026-07-17_14-30-00/  # 快  1（原 备份文件）
    ├── 2026-07-17_15-00-00/  # 快  2
    └── ...
```

---

## 工作流程

```mermaid
sequenceDiagram
    participant User
    participant Tool as agent-nexus
    participant Proxy as LLM 代理<br/>(CCX Desktop 或自定义)
    participant Backend as 后端模型
    participant FS as 文件系统/备份

    User->>Tool: agent-nexus conf bak
    Tool->>Proxy: 检测代理配置（--url/--key 或自动嗅探）
    Proxy-->>Tool: URL / Key / 模型映射?
    Tool->>FS: 扫描已安装的 agent
    FS-->>Tool: agent 列表 + 配置文件路径
    Tool->>FS: 创建配置快 （versioning.json + snapshots/?
    FS-->>Tool: 快  ID
    Tool->>FS: 备份现有配置
    FS-->>Tool: 备份完成
    Tool->>FS: 逐个配置可配置的 agent
    FS-->>Tool: 配置结果（成?跳过?
    Tool-->>User: 显示配置结果 + 模型路由?
    User->>Backend: 使用 agent 调用 LLM
    Backend-->>User: 响应
```

---

## 扩展?Agent

实现 `agent.ConfigWriter` 接口并注册到 `WriterRegistry` 即可?

```go
type myAgentWriter struct{}

func newMyAgentWriter() *myAgentWriter { return &myAgentWriter{} }

func (w *myAgentWriter) Name() string     { return "myagent" }
func (w *myAgentWriter) Category() string { return "cli" }
func (w *myAgentWriter) CanConfigure(p *proxy.Proxy) bool { return true }
func (w *myAgentWriter) Configure(path string, p *proxy.Proxy) error { /* 写入逻辑 */ }
func (w *myAgentWriter) Status(path string) (bool, string) { /* 状态检?*/ }
```

然后?`agent.go` ?`NewWriterRegistry()` 中注册：

```go
writers: []ConfigWriter{
    // ... 现有写入?
    newMyAgentWriter(),
},
```

---

## 注意事项

- CCX Desktop 需保持运行（监?`127.0.0.1:3688`），或使?`--url` 指定自定义代?
- Cursor 的字段名取决于版本，不匹配时需通过 Cursor 设置 UI 手动填入
- `conf bak` 会先自动创建快照再配置 agent，无需单独调用 configure
- **每次配置仅支持一个代?*，所?agent 共享同一个代理地址
- 配置快 存储?`~/.codex/backups/`，使?`agent-nexus conf history` 查看所有快?
- 敏感信息（API Key）仅写入?agent 自身配置文件，未扩散
- 配置生效前所有原 配置文件均已备份并创建快 ，可随时回滚
- **OpenClaude** 配置写入 `~/.openclaude-env` 文件?env 格式），启动时需指定：`openclaude --provider-env-file ~/.openclaude-env`。也可设置系统环境变?`CLAUDE_CODE_USE_OPENAI=1`、`OPENAI_API_KEY`、`OPENAI_BASE_URL`、`OPENAI_MODEL` 后直接运?`openclaude`

