# agent-nexus 用户使用手册

## 核心概念

### 聚合网关（Proxy）
CCX Desktop 作为本地代理服务器运行于 `127.0.0.1:3688`，上游连接 sensenova（`https://token.sensenova.cn`）。所有 agent 通过它统一访问 LLM 后端。

### Agent 配置
每个 AI agent 有自己的配置文件（TOML / JSON），记录了 API 地址、Key 和模型名。

### 嗅探（Detect）
工具自动读取 CCX Desktop 的 `config.json` 和 `.env` 文件，获取代理 URL、Key 和模型映射表。

### 备份
每次配置生效前，自动将原始配置文件备份到 `~/.codex/backups/`，带时间戳，方便回滚。

## 快速开始

### 用法 1：一键完整配置

```powershell
.\agent-nexus.exe configure
```

完整流程：检测代理 → 扫描 agent → 备份 → 配置所有可配置的 agent → 显示路由表。

### 用法 2：手动指定代理

```powershell
.\agent-nexus.exe configure --url http://127.0.0.1:8080/v1 --key sk-your-key
```

直接传入 URL 和 Key，跳过自动嗅探。

### 用法 3：查看当前状态

```powershell
.\agent-nexus.exe status
.\agent-nexus.exe route
```

## 完整命令参考

### configure — 备份后一键自动配置

```powershell
.\agent-nexus.exe configure
.\agent-nexus.exe configure --url http://proxy:9000/v1 --key sk-xxx
```

流程：
1. 检测 CCX Desktop 代理（或用 `--url`/`--key` 指定）
2. 扫描已安装的 AI agent
3. 备份所有现有配置文件
4. 逐个配置可配置的 agent
5. 显示配置结果和模型路由表

### backup — 备份所有 agent 配置

```powershell
.\agent-nexus.exe backup
```

将所有 agent 配置文件复制到 `~/.codex/backups/agent-configs-YYYY-MM-DD_HH-MM-SS/`。

### status — 显示各 agent 状态

```powershell
.\agent-nexus.exe status
```

输出每个 agent 的安装状态和配置状态（🔗 已配置 / ⚙️ 未配置 / ❌ 未安装）。

### route — 显示模型路由表

```powershell
.\agent-nexus.exe route
```

显示三层模型路由：各 agent 的写入模型 → 实际后端模型，以及 CCX 自动映射表。

### discover — 扫描已安装的 agent

```powershell
.\agent-nexus.exe discover
```

列出所有已扫描到的 AI agent 及其配置文件路径。

### detect — 检测代理配置

```powershell
.\agent-nexus.exe detect
.\agent-nexus.exe detect --url http://proxy:9000/v1 --key sk-xxx
```

读取 CCX Desktop 配置并输出代理地址、Key 和模型映射表。

## 工作流程

```
┌─────────────┐    ┌──────────────┐    ┌─────────────┐    ┌──────────────┐
│  CCX Desktop │ ←─ │  go-agent-   │ →  │  Agent 配置  │ ←─ │  原始备份     │
│  (代理网关)   │    │  config       │    │  (已配置)    │    │  (.codex/    │
└─────────────┘    └──────────────┘    └─────────────┘    │   backups/   │
      ↑                                           ↑        └──────────────┘
      │                                           │
      └──────── sensenova 上游 ────────────────────┘
```

## 备份与恢复详细操作

### 自动备份
每次执行 `configure` 或 `backup` 时，工具会自动将每个 agent 的配置文件复制到：

```
C:\Users\<用户名>\.codex\backups\agent-configs-2026-07-17_14-30-00\
```

目录包含：
- `config.toml` — Codex 配置
- `settings.json` — Claude 配置
- 等

### 回滚（恢复）

```powershell
# 找到最新的备份目录
cd ~/.codex/backups
# 复制备份文件回原位
copy .\agent-configs-2026-07-17_14-30-00\config.toml ~/.codex\config.toml
```

## 典型使用场景

### 场景 1：首次使用
```powershell
.\agent-nexus.exe configure
```

### 场景 2：更换代理地址
```powershell
.\agent-nexus.exe configure --url http://127.0.0.1:9000/v1 --key new-key
```

### 场景 3：只查看状态（不修改）
```powershell
.\agent-nexus.exe status
.\agent-nexus.exe route
```

### 场景 4：只备份（不配置）
```powershell
.\agent-nexus.exe backup
```

## 彩色输出含义

| 符号 | 含义 |
|------|------|
| ✅ | agent 已安装 |
| 🔗 | agent 已配置代理 |
| ⚙️ | agent 已安装但未配置代理 |
| ❌ | agent 未安装 |
| ⚠ | 无法配置（使用自有 AI 后端） |

## 故障排查

### 配置失败：无法连接到代理
确保 CCX Desktop 正在运行（`127.0.0.1:3688`）。

### 配置失败：API Key 无效
检查 CCX Desktop 的 `.env` 文件中 `PORT=` 和 `API_KEY=` 设置。

### Cursor 配置不生效
Cursor 的字段名取决于版本。如果自动配置无效，请通过 Cursor 设置 UI → OpenAI Compatible 手动填入相同的 base URL 和 key。

### 备份目录找不到
备份路径为 `~/.codex/backups/`，在 Windows 上即 `C:\Users\<用户名>\.codex\backups\`。

## 安全注意事项

- API Key 仅写入各 agent 自身的配置文件，不会扩散到其他地方
- 配置文件均为本地文件，不上传网络
- 备份目录仅本地存储，不对外暴露

