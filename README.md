# agent-nexus — AI Agent 配置自动化工具

> 一个代理，统治它们。

---

## 为什么存在

你电脑上装了 codex、claude code、kimi、deepseek、opencode、cursor、openclaw……它们各自需要一个 API key 和 endpoint。管理 10+ 个 agent 的配置？手动改 JSON/YAML/TOML？每次换代理就重新配一遍？

**这是 agent-nexus 存在的唯一理由：** 把 LLM 代理和 coding agent 之间的连接问题，从"逐个手配"变成"一次配置，全部生效"。

---

## 核心概念：AI 消息网关 vs Agent 运行时

```
┌─────────────────────────────────────────────────────────────────┐
│                        AI 消息网关 (LLM Proxy)                    │
│                                                                  │
│  ┌──────────┐   ┌──────────┐   ┌──────────┐   ┌──────────────┐  │
│  │ CCX      │   │ CC-Switch│   │ Sensitive│   │ Custom Proxy │  │
│  │ Desktop  │   │          │   │ Nova     │   │              │  │
│  └────┬─────┘   └────┬─────┘   └────┬─────┘   └──────┬───────┘  │
│       │              │              │                │          │
│       └──────────────┼──────────────┼────────────────┘          │
│                      ▼              ▼                           │
│              ┌──────────────┐   ┌──────────────┐                │
│              │ 统一 endpoint │   │ 模型映射表   │                │
│              │  + 统一 key   │   │ (model mux)  │                │
│              └──────────────┘   └──────────────┘                │
└─────────────────────────────────────────────────────────────────┘
                              │
                              │ agent-nexus 配置层
                              ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Agent 运行时 (Coding Agents)                  │
│                                                                  │
│  codex  │ claude │ kimi │ deepseek │ opencode │ openclaw        │
│  cursor │ codebuddy │ hermes │ kiro │ grok │ qoder │ trae      │
│                                                                  │
│  每个 agent 都有自己的配置格式（JSON/YAML/TOML/.env），             │
│  agent-nexus 负责把它们全部指向同一个 AI 消息网关                    │
└─────────────────────────────────────────────────────────────────┘
```

- **AI 消息网关**（proxy）：统一的上游端点，负责模型路由和计费。你只需要关心"用哪个模型"，不需要关心"调哪个 API"。
- **Agent 运行时**（agent）：你日常使用的 coding 工具。它们各有各的配置格式，但本质上都是"调一个 LLM endpoint"。
- **agent-nexus** 是中间件：发现本机所有 agent → 检测代理 → 自动备份 → 统一重写配置文件。

---

## 一句话

agent-nexus = AI 代理配置领域的 **`apt-get upgrade`**：
一条命令，扫描本机所有 AI agent，把它们全部接入同一个代理。

详细使用方式见 [MANUAL.md](MANUAL.md)。

---

## 项目结构

```
agent-nexus/
├── main.go                          # 入口
├── cmd/
│   └── root.go                      # Cobra CLI 命令定义
└── internal/
    ├── agent/                       # 各 agent 配置写入器（可插拔）
    ├── backup/                      # 备份逻辑
    ├── color/                       # 终端彩色输出
    ├── discover/                    # 自动发现 agent
    ├── model/                       # 模型路由表构建
    ├── proxy/                       # 代理检测（CCX / 自定义）
    ├── sniff/                       # LLM endpoint 嗅探
    └── versioning/                  # 配置版本化（快照/分支/差异）
```

## 扩展新 Agent

实现 `agent.ConfigWriter` 接口并注册到 `WriterRegistry` 即可，参考 [MANUAL.md](MANUAL.md#扩展新-agent)。

## License

MIT
