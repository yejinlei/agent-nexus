package cmd

import (
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "runtime"
    "sort"
    "strings"
    "time"

    "github.com/spf13/cobra"
    "agent-nexus/internal/agent"
    "agent-nexus/internal/backup"
    "agent-nexus/internal/discover"
    "agent-nexus/internal/install"
    "agent-nexus/internal/model"
    "agent-nexus/internal/proxy"
    "agent-nexus/internal/sniff"
    "agent-nexus/internal/versioning"
)

var homeDir string

var rootCmd = &cobra.Command{
    Use:   "agent-nexus",
    Short: "AI Agent Configuration Tool - 自动化配置各种 AI coding agent",
    Long: `agent-nexus - 一键自动化配置各种 AI coding agent

功能：
  1. 自动发现本机已安装的 AI agent (codex, claude, kimi, deepseek, opencode 等)
  2. 自动检测 AI 代理配置 (URL, Key, 模型映射)
  3. 配置生效前自动备份原有配置（支持版本化管理）
  4. 将 AI 代理配置写入各 agent 配置文件
  5. 自动模型重定向，匹配最佳后端模型
  6. 配置文件版本管理：快照、回滚、分支、差异对比
  7. 嗅探 LLM 提供商消息格式与模型列表

支持的 agent:
  CLI:  codex, claude, kimi, deepseek, opencode, openclaw,
        codebuddy, hermes, kiro, grok, qoder, trae
  IDE:  cursor (via openai-compatible provider)
  不可配置: antigravity, copilot, deveco, pi, qoder-ide, trae-ide,
            codebuddy-ide, windsurf, zed

用法：
  agent-nexus discover [-v]   扫描已安装的 agent（-v 显示支持模型）
  agent-nexus detect         检测 AI 代理配置
  agent-nexus backup         备份所有配置（自动版本化）
  agent-nexus configure      备份后自动配置指定的 agent（必选 --agents 参数）
  agent-nexus status         显示配置状态
  agent-nexus route          显示模型路由表
  agent-nexus snapshot       创建配置快照
  agent-nexus restore        恢复到指定快照
  agent-nexus version        列出所有配置快照
  agent-nexus diff           对比两个快照的差异
  agent-nexus branch         管理配置分支
  agent-nexus sniff          嗅探 LLM 提供商消息格式与模型
`,
}

var proxySettings *proxy.Proxy

var installAll bool
var installExecute bool = true
var installForce bool

func init() {
    rootCmd.PersistentFlags().StringVar(&homeDir, "home", "", "user home directory (auto-detected by default)")
    rootCmd.PersistentFlags().StringVar(&proxyURL, "url", "", "directly specify proxy URL (overrides auto-detect)")
    rootCmd.PersistentFlags().StringVar(&proxyKey, "key", "", "directly specify proxy API key (overrides auto-detect)")
}

var proxyURL string
var proxyKey string


func userHomeDir() string {
    if homeDir != "" {
        return homeDir
    }
    h, _ := os.UserHomeDir()
    return h
}

func getProxySettings() (*proxy.Proxy, error) {
    if proxyURL != "" || proxyKey != "" {
        return proxy.FromFlags(proxyURL, proxyKey)
    }
    return proxy.Detect()
}

// discoverCmd scans for installed agents
var (
    discoverVerbose bool
)

var discoverCmd = &cobra.Command{
    Use:   "discover",
    Short: "扫描并列出已安装的 AI agent",
    RunE: func(cmd *cobra.Command, args []string) error {
        agents := discover.Discover()

        // Default mode: summary table
        discover.RenderTable(agents)

        // Verbose mode: full detail table with model routing
        if discoverVerbose {
            fmt.Printf("正在检测 AI 代理以获取模型信息...")
            p, err := getProxySettings()
            if err != nil {
                fmt.Printf("  未检测到 AI 代理配置（将仅显示默认模型）`n")
            } else {
                fmt.Printf("  代理: %s (%s)`n", p.Source, p.BaseURL)
            }

            fmt.Printf("`n模型支持详情:`n")
            discover.RenderVerboseTable(agents)

            // Build and display the routing table
            routing := model.BuildRoutingTable(p)
            fmt.Println("模型路由表:")
            fmt.Println(strings.Repeat("-", 70))
            for _, r := range routing {
                fmt.Printf("  %-10s %-28s → %-28s [%s]`n", r.Agent, r.Model, r.Target, r.Source)
            }
            fmt.Println()
        }

        return nil
    },
}
// detectCmd auto-detects AI proxy settings
var detectCmd = &cobra.Command{
    Use:   "detect",
    Short: "检测 AI 代理配置 (URL, Key, 模型映射)",
    RunE: func(cmd *cobra.Command, args []string) error {
        p, err := getProxySettings()
        if err != nil {
            fmt.Printf("未能检测到 AI 代理配置: %v\n", err)
            return err
        }
        fmt.Printf("\nAI 代理配置已检测:\n")
        fmt.Printf("  地址:   %s\n", p.BaseURL)
        fmt.Printf("  端口:   %d\n", p.Port)
        fmt.Printf("  密钥:   %s\n", p.APIKey)
        fmt.Printf("\n  模型映射表 (%d 条):\n", len(p.ModelMap))
        for src, dst := range p.ModelMap {
            fmt.Printf("    %-15s → %s\n", src, dst)
        }
        fmt.Println()
        return nil
    },
}

// backupCmd backs up all agent configs with versioning
var (
    backupBranch  string
    backupMessage string
)

var backupCmd = &cobra.Command{
    Use:   "backup",
    Short: "备份所有 agent 配置文件（带版本信息）",
    Long: `备份所有已安装 agent 的配置文件，自动生成版本化快照。

快照元数据存储在 ~/.codex/backups/versioning.json
原始备份文件存储在 ~/.codex/backups/snapshots/<时间戳>/

示例:
  agent-nexus backup                                          # 默认分支 main
  agent-nexus backup --branch production                      # 指定分支
  agent-nexus backup --message "配置更新前快照"                 # 添加提交信息
  agent-nexus backup --branch pre-release --message "PR v2.0"  # 分支+信息
`,
    RunE: func(cmd *cobra.Command, args []string) error {
        home := userHomeDir()
        destRoot := filepath.Join(home, ".codex", "backups")

        agents := discover.Discover()
        var paths []string
        for _, a := range agents {
            if a.HasConfig {
                paths = append(paths, a.ConfigPath)
            }
        }

        if len(paths) == 0 {
            fmt.Println("未发现可备份的配置文件。")
            return nil
        }

        r := versioning.LoadRegistry(destRoot)
        s, err := r.CreateSnapshot(paths, backupMessage, backupBranch)
        if err != nil {
            fmt.Printf("创建快照失败: %v\n", err)
            return err
        }

        fmt.Printf("\n快照已创建: %s (分支: %s)\n", s.ID, s.Branch)
        fmt.Println(strings.Repeat("-", 60))

        for _, p := range paths {
            entry, ok := s.Configs[filepath.Base(p)]
            if !ok {
                fmt.Printf("  ⚠ %s: 未捕获\n", filepath.Base(p))
                continue
            }
            if entry.Error != "" {
                fmt.Printf("  ⚠ %s: %s\n", filepath.Base(p), entry.Error)
                continue
            }
            fmt.Printf("  ✅ %s  [%s, %d bytes, sha256=%s...]\n",
                filepath.Base(p), entry.SHA256[:8], entry.Bytes, entry.SHA256[:8])
        }
        fmt.Printf("\n消息: %s\n", s.Message)
        fmt.Printf("快照数: %d\n", len(r.ListSnapshots()))
        return nil
    },
}

// snapshotCmd creates a named snapshot
var (
    snapshotBranch  string
    snapshotMessage string
)

var snapshotCmd = &cobra.Command{
    Use:   "snapshot",
    Short: "创建配置快照（快照/提交）",
    Long: `创建配置快照，类似 git commit。快照包含所有可配置 agent 的当前配置内容和元数据。

快照会自动保存到 ~/.codex/backups/snapshots/<时间戳>/
元数据存储在 ~/.codex/backups/versioning.json

示例:
  agent-nexus snapshot --message "初始配置"
  agent-nexus snapshot --branch dev --message "开发分支配置"
`,
    RunE: func(cmd *cobra.Command, args []string) error {
        home := userHomeDir()
        destRoot := filepath.Join(home, ".codex", "backups")

        agents := discover.Discover()
        var paths []string
        for _, a := range agents {
            if a.HasConfig && a.IsConfigurable {
                paths = append(paths, a.ConfigPath)
            }
        }

        if len(paths) == 0 {
            fmt.Println("未发现可配置的 agent 配置文件。")
            return nil
        }

        r := versioning.LoadRegistry(destRoot)
        s, err := r.CreateSnapshot(paths, snapshotMessage, snapshotBranch)
        if err != nil {
            fmt.Printf("创建快照失败: %v\n", err)
            return err
        }

        fmt.Printf("\n✅ 快照已创建: %s (分支: %s)\n", s.ID, s.Branch)
        fmt.Println(strings.Repeat("-", 60))

        for _, p := range paths {
            entry, ok := s.Configs[filepath.Base(p)]
            if !ok {
                continue
            }
            if entry.Error != "" {
                fmt.Printf("  ⚠ %s: %s\n", filepath.Base(p), entry.Error)
                continue
            }
            fmt.Printf("  ✅ %s [%s, %d bytes]\n", filepath.Base(p), entry.SHA256[:8], entry.Bytes, entry.SHA256[:8])
        }
        fmt.Printf("\n提交信息: %s\n", s.Message)
        fmt.Printf("总快照数: %d\n", len(r.ListSnapshots()))
        return nil
    },
}

// restoreCmd restores config files from a snapshot
var restoreID string

var restoreCmd = &cobra.Command{
    Use:   "restore",
    Short: "恢复到指定快照",
    Long: `从指定的历史快照恢复 agent 配置文件。

使用 'agent-nexus version' 查看所有可用的快照 ID。

示例:
  agent-nexus restore --snapshot 2026-07-17_14-30-00              # 恢复指定快照
  agent-nexus restore --snapshot latest                            # 恢复到最新快照
  agent-nexus version                                             # 先查看可用快照
`,
    RunE: func(cmd *cobra.Command, args []string) error {
        if restoreID == "" {
            return fmt.Errorf("请指定快照 ID（使用 --snapshot 参数，或输入 'latest' 恢复最新快照）")
        }

        home := userHomeDir()
        destRoot := filepath.Join(home, ".codex", "backups")
        r := versioning.LoadRegistry(destRoot)

        targetID := restoreID
        if strings.EqualFold(targetID, "latest") {
            latest := r.LatestSnapshot()
            if latest == nil {
                return fmt.Errorf("未找到任何快照")
            }
            targetID = latest.ID
            fmt.Printf("自动选择最新快照: %s\n", targetID)
        }

        s := r.GetSnapshot(targetID)
        if s == nil {
            return fmt.Errorf("快照 %s 不存在", targetID)
        }

        fmt.Printf("\n正在恢复到快照: %s (分支: %s)\n", s.ID, s.Branch)
        fmt.Printf("提交信息: %s\n", s.Message)
        fmt.Println(strings.Repeat("-", 60))

        restored, err := r.RestoreSnapshot(targetID)
        if err != nil {
            return err
        }

        fmt.Printf("\n✅ 已恢复 %d 个配置文件\n", len(restored))
        fmt.Println()
        return nil
    },
}

// versionCmd lists all configuration snapshots
var versionCmd = &cobra.Command{
    Use:   "version",
    Short: "列出所有配置快照（版本历史）",
    Long: `显示所有历史配置快照，包括时间戳、分支、提交信息和包含的文件。

示例:
  agent-nexus version                                          # 显示所有快照
  agent-nexus version --branch main                            # 只显示主分支
`,
    RunE: func(cmd *cobra.Command, args []string) error {
        home := userHomeDir()
        destRoot := filepath.Join(home, ".codex", "backups")
        r := versioning.LoadRegistry(destRoot)

        fmt.Printf("\n配置版本历史 (%d 个快照):\n", len(r.Snapshots))
        fmt.Println(strings.Repeat("-", 80))

        snapshots := r.ListSnapshots()
        if len(snapshots) == 0 {
            fmt.Println("  无快照。使用 'agent-nexus backup' 或 'agent-nexus snapshot' 创建第一个快照。")
            fmt.Println()
            return nil
        }

        for i, s := range snapshots {
            icon := ""
            if i == 0 {
                icon = "◀"
            }
            fmt.Printf("\n  [%s] %s | 分支: %s\n", icon, s.ID, s.Branch)
            fmt.Printf("       时间: %s  信息: %s\n",
                s.CreatedAt.Format("2006-01-02 15:04:05"), s.Message)
            fmt.Printf("       文件 (%d):\n", len(s.Configs))

            for name, entry := range s.Configs {
                if entry.Error != "" {
                    fmt.Printf("        ⚠ %s: %s\n", name, entry.Error)
                    continue
                }
                fmt.Printf("        %s  [%s, %d bytes]\n", name, entry.SHA256[:8], entry.Bytes)
            }
        }

        // Show branch info
        if len(r.Branches) > 1 {
            fmt.Printf("\n  可用分支: %s\n", strings.Join(r.BranchesList(), ", "))
            fmt.Printf("  当前分支: %s\n", r.CurrentBranch)
        }

        fmt.Println()
        return nil
    },
}

// diffCmd compares two snapshots
var (
    diffOld string
    diffNew string
)

var diffCmd = &cobra.Command{
    Use:   "diff",
    Short: "对比两个快照的差异",
    Long: `比较两个版本快照之间的配置变更，显示新增、删除和修改的文件。

使用 'agent-nexus version' 查看所有可用快照 ID。
使用 'latest' 表示最新快照。

示例:
  agent-nexus diff --old 2026-07-17_14-30-00 --new 2026-07-17_15-00-00
  agent-nexus diff --old latest --new 2026-07-17_14-30-00       # 对比最新与指定版本
  agent-nexus diff --old 2026-07-17_14-30-00 --new latest        # 指定版本对比最新
`,
    RunE: func(cmd *cobra.Command, args []string) error {
        if diffOld == "" || diffNew == "" {
            return fmt.Errorf("请指定 --old 和 --new 快照 ID（使用 'agent-nexus version' 查看可用快照）")
        }

        home := userHomeDir()
        destRoot := filepath.Join(home, ".codex", "backups")
        r := versioning.LoadRegistry(destRoot)

        oldID := diffOld
        newID := diffNew
        if strings.EqualFold(oldID, "latest") {
            latest := r.LatestSnapshot()
            if latest == nil {
                return fmt.Errorf("--old 指定 'latest' 但未找到任何快照")
            }
            oldID = latest.ID
        }
        if strings.EqualFold(newID, "latest") {
            latest := r.LatestSnapshot()
            if latest == nil {
                return fmt.Errorf("--new 指定 'latest' 但未找到任何快照")
            }
            newID = latest.ID
        }

        oldSnap := r.GetSnapshot(oldID)
        newSnap := r.GetSnapshot(newID)
        if oldSnap == nil {
            return fmt.Errorf("旧快照 %s 不存在", diffOld)
        }
        if newSnap == nil {
            return fmt.Errorf("新快照 %s 不存在", diffNew)
        }

        diffs, err := r.SnapshotDiff(oldID, newID)
        if err != nil {
            return err
        }

        fmt.Printf("\n快照差异: %s → %s\n", oldID, newID)
        fmt.Printf("旧: %s (%s)  新: %s (%s)\n",
            oldSnap.CreatedAt.Format("2006-01-02 15:04:05"), oldSnap.Message,
            newSnap.CreatedAt.Format("2006-01-02 15:04:05"), newSnap.Message)
        fmt.Println(strings.Repeat("-", 60))

        added := 0
        removed := 0
        modified := 0
        unchanged := 0

        for _, d := range diffs {
            switch d.Status {
            case "added":
                fmt.Printf("  [+] %s (%d bytes)\n", d.Agent, d.NewSize)
                added++
            case "removed":
                fmt.Printf("  [-] %s (%d bytes)\n", d.Agent, d.OldSize)
                removed++
            case "modified":
                fmt.Printf("  [M] %s  [%s → %s] (%d → %d bytes)\n",
                    d.Agent, d.OldSHA256[:8], d.NewSHA256[:8], d.OldSize, d.NewSize)
                modified++
            case "error":
                fmt.Printf("  [?] %s: %s\n", d.Agent, d.Message)
            default:
                fmt.Printf("  [ ] %s (未变更)\n", d.Agent)
                unchanged++
            }
        }

        fmt.Printf("\n变更统计: +added %d  -removed %d  Mmodified %d  =unchanged %d\n",
            added, removed, modified, unchanged)
        fmt.Println()
        return nil
    },
}

// branchCmd manages configuration branches
var (
    branchCreateName string
    branchSwitchName string
    branchShow       bool
)

var branchCmd = &cobra.Command{
    Use:   "branch",
    Short: "管理配置分支",
    Long: `管理配置快照的分支，类似 git branch。

用法:
  agent-nexus branch create <name>     创建新分支
  agent-nexus branch switch <name>     切换到指定分支
  agent-nexus branch list              列出所有分支
  agent-nexus branch show              显示当前分支信息

示例:
  agent-nexus branch create production    # 创建生产分支
  agent-nexus branch switch production    # 切换到生产分支
  agent-nexus snapshot --branch production  # 在指定分支上创建快照
`,
    RunE: func(cmd *cobra.Command, args []string) error {
        home := userHomeDir()
        destRoot := filepath.Join(home, ".codex", "backups")
        r := versioning.LoadRegistry(destRoot)

        if branchCreateName != "" {
            if _, ok := r.Branches[branchCreateName]; ok {
                fmt.Printf("分支 %s 已存在\n", branchCreateName)
                return nil
            }
            r.Branches[branchCreateName] = &versioning.Branch{
                Name:      branchCreateName,
                CreatedAt: time.Now(),
            }
            if err := r.Save(); err != nil {
                return err
            }
            fmt.Printf("✅ 已创建分支: %s\n", branchCreateName)
            return nil
        }

        if branchSwitchName != "" {
            if err := r.CheckoutBranch(branchSwitchName); err != nil {
                return err
            }
            fmt.Printf("✅ 已切换到分支: %s\n", branchSwitchName)
            return nil
        }

        if branchShow {
            fmt.Printf("当前分支: %s\n", r.CurrentBranch)
            if r.Branches[r.CurrentBranch] != nil {
                b := r.Branches[r.CurrentBranch]
                fmt.Printf("创建时间: %s\n", b.CreatedAt.Format("2006-01-02 15:04:05"))
            }
            return nil
        }

        // Default: list branches
        fmt.Printf("\n可用分支 (%d):\n", len(r.Branches))
        fmt.Println(strings.Repeat("-", 40))
        for _, name := range r.BranchesList() {
            marker := ""
            if name == r.CurrentBranch {
                marker = " ◀"
            }
            b := r.Branches[name]
            fmt.Printf("  %-20s %s %s\n", name, marker, b.CreatedAt.Format("2006-01-02"))
        }
        fmt.Printf("\n当前分支: %s\n", r.CurrentBranch)
        fmt.Println()
        return nil
    },
}

// configureCmd backs up then configures selected agents (required --agents flag)
var (
    configureAgents string
    configureModels string  // optional: "agent=model,agent2=model2"
)

var configureCmd = &cobra.Command{
    Use:   "configure",
    Short: "备份后自动配置指定的 agent（必选 --agents 参数）",
    Long: `agent-nexus configure --agents <agent1[,agent2,...]|all>

必选参数:
  --agents  要配置的 agent 名称（逗号分隔）或 all 表示配置所有已安装的 agent

可选参数:
  --models  用 模型名 覆盖默认映射，格式: "agent=模型名,agent2=模型名2"
            如: --models "codex=gpt-5.5,claude=deepseek-v4-flash"

配置前会自动创建配置快照，支持后续回滚。

示例:
  agent-nexus configure --agents all              # 配置所有已安装的 agent
  agent-nexus configure --agents claude,kimi      # 仅配置 Claude 和 Kimi
  agent-nexus configure --agents codex             # 仅配置 Codex
  agent-nexus configure --agents all --models "codex=gpt-5.5"  # 覆盖模型
`,
    RunE: func(cmd *cobra.Command, args []string) error {
        if configureAgents == "" {
            return fmt.Errorf("--agents 为必选参数，请指定要配置的 agent（使用 all 配置所有）")
        }

        home := userHomeDir()
        destRoot := filepath.Join(home, ".codex", "backups")

        fmt.Println("[1/6] 检测 AI 代理...")
        p, err := getProxySettings()
        if err != nil {
            proxySettings = p
            fmt.Printf("❌ 未检测到 AI proxy 配置: %v\n", err)
            fmt.Println("   请确保 AI 代理已安装并运行")
            return err
        }
        proxySettings = p
        fmt.Printf("  ✅ 代理类型: %s  地址: %s  密钥: %s\n", p.Source, p.BaseURL, p.APIKey)
        fmt.Println()

        // Fetch upstream model list for resolution
        fmt.Println("  正在获取上游模型列表...")
        upstreamModels := sniff.UpstreamModelList(p.BaseURL, p.APIKey)
        if len(upstreamModels) > 0 {
            fmt.Printf("  上游可用模型 (%d): %v\n", len(upstreamModels), upstreamModels)
        } else {
            fmt.Println("  ⚠ 无法获取上游模型列表（将仅使用代理模型映射）")
        }
        fmt.Println()

        fmt.Println("[2/6] 扫描已安装的 agent...")
        agents := discover.Discover()
        fmt.Printf("  发现 %d 个 agent\n\n", len(agents))

        // Build set of selected agent names
        var selectedNames []string
        if strings.EqualFold(configureAgents, "all") {
            for _, a := range agents {
                if a.HasConfig && a.IsConfigurable {
                    selectedNames = append(selectedNames, a.Name)
                }
            }
        } else {
            selectedNames = strings.Split(configureAgents, ",")
            for i, n := range selectedNames {
                selectedNames[i] = strings.TrimSpace(n)
            }
            seen := map[string]bool{}
            var deduped []string
            for _, n := range selectedNames {
                if !seen[n] {
                    seen[n] = true
                    deduped = append(deduped, n)
                }
            }
            selectedNames = deduped
        }
        sort.Strings(selectedNames)
        fmt.Printf("  目标 agent: %s\n\n", strings.Join(selectedNames, ", "))

        // Filter: only installed agents
        nameToAgent := map[string]discover.AgentInfo{}
        for _, a := range agents {
            nameToAgent[a.Name] = a
        }

        var toConfigure []discover.AgentInfo
        for _, name := range selectedNames {
            a, ok := nameToAgent[name]
            if !ok {
                fmt.Printf("  ⚠ %s: 未检测到该 agent，跳过\n", name)
                continue
            }
            if !a.HasConfig {
                fmt.Printf("  ⚠ %s: 未安装，跳过\n", name)
                continue
            }
            if !a.IsConfigurable {
                fmt.Printf("  ⚠ %s: 不可配置，跳过\n", name)
                continue
            }
            toConfigure = append(toConfigure, a)
        }

        if len(toConfigure) == 0 {
            fmt.Println("\n没有可配置的 agent，退出。")
            return nil
        }

        // [3/6] Resolve model mappings
        fmt.Println("[3/6] 解析模型映射...")
        resolutions := model.ResolveAllModels(upstreamModels, p.ModelMap)
        fmt.Println()

        // Parse --models overrides
        overrides := make(map[string]string)
        if configureModels != "" {
            for _, pair := range strings.Split(configureModels, ",") {
                pair = strings.TrimSpace(pair)
                if idx := strings.Index(pair, "="); idx > 0 {
                    agent := strings.TrimSpace(pair[:idx])
                    m := strings.TrimSpace(pair[idx+1:])
                    if m != "" {
                        overrides[agent] = m
                    }
                }
            }
        }

        // Show resolution table
        fmt.Println("  模型映射预览:")
        fmt.Printf("  %-14s %-30s [%s]\n", "Agent", "模型", "来源")
        fmt.Println(strings.Repeat("-", 70))
        for _, r := range resolutions {
            displayModel := r.Model
            src := r.Source
            if ov, ok := overrides[r.Agent]; ok {
                displayModel = ov
                src = "override"
            }
            if src == "upstream" {
                src = "上游直接"
            } else if src == "proxy-map" {
                src = "代理重定向"
            } else if src == "default" {
                src = "默认"
            }
            fmt.Printf("  %-14s %-30s [%s]\n", r.Agent, displayModel, src)
            if r.Notes != "" {
                fmt.Printf("    └─ %s\n", r.Notes)
            }
        }
        fmt.Println()

        // [4/6] Create snapshot
        fmt.Println("[4/6] 创建配置快照...")
        r := versioning.LoadRegistry(destRoot)
        var snapshotPaths []string
        for _, a := range toConfigure {
            if a.HasConfig {
                snapshotPaths = append(snapshotPaths, a.ConfigPath)
            }
        }
        _, err = r.CreateSnapshot(snapshotPaths, fmt.Sprintf("自动配置快照: %s", strings.Join(selectedNames, ",")), "")
        if err != nil {
            fmt.Printf("  ⚠ 快照创建失败: %v\n", err)
        } else {
            fmt.Printf("  ✅ 快照已创建（可在配置失败时回滚）\n")
        }
        fmt.Println()

        // [5/6] Backup
        fmt.Println("[5/6] 备份现有配置（兼容旧格式）...")
        var backupPaths []string
        for _, a := range toConfigure {
            if a.HasConfig {
                backupPaths = append(backupPaths, a.ConfigPath)
            }
        }

        if len(backupPaths) > 0 {
            results, err := backup.Backup(backupPaths, filepath.Join(home, ".codex"))
            if err != nil {
                fmt.Printf("  ⚠ 备份失败: %v\n", err)
            } else {
                for _, result := range results {
                    if result.Success {
                        fmt.Printf("  ✅ %s\n", filepath.Base(result.Source))
                    }
                }
            }
        }
        fmt.Println()

        // [6/6] Configure
        fmt.Println("[6/6] 配置 agent...")
        reg := agent.NewWriterRegistry()
        configured := 0
        skipped := 0

        for _, a := range toConfigure {
            writer := reg.Get(a.Name)
            if writer == nil {
                fmt.Printf("  ⚠ %s: 无对应配置写入器\n", a.Name)
                skipped++
                continue
            }
            if !writer.CanConfigure(p) {
                fmt.Printf("  ⚠ %s: 当前代理不支持配置\n", a.Name)
                skipped++
                continue
            }

            // Resolve model for this agent
            resolvedModel, found := model.ModelToWrite(resolutions, overrides, a.Name)
            if !found {
                resolvedModel = ""
            }

            err := writer.Configure(a.ConfigPath, p, resolvedModel)
            if err != nil {
                fmt.Printf("  ❌ %s: %v\n", a.Name, err)
                fmt.Println("  提示: 使用 'agent-nexus restore latest' 回滚到此操作前的快照")
                skipped++
            } else {
                wmodel, wsrc, wnotes := writer.StatusModel(a.ConfigPath)
                _, status := writer.Status(a.ConfigPath)
                fmt.Printf("  ✅ %s → %s [%s: %s]\n", a.Name, status, wmodel, wsrc)
                if wnotes != "" {
                    fmt.Printf("    └─ %s\n", wnotes)
                }
                configured++
            }
        }

        fmt.Printf("\n配置完成: %d 个 agent 已配置, %d 个跳过\n", configured, skipped)
        if skipped > 0 {
            fmt.Println("如需回滚: agent-nexus restore latest")
        }

        // Show updated routing table
        fmt.Println("\n更新后模型路由表:")
        routing := model.BuildRoutingTable(p)
        for _, r := range routing {
            fmt.Printf("  %-10s %-30s → %-30s [%s]\n", r.Agent, r.Model, r.Target, r.Source)
        }
        fmt.Println()
        return nil
    },
}

// statusCmd shows current configuration status
var statusCmd = &cobra.Command{
    Use:   "status",
    Short: "显示所有 agent 的当前配置状态",
    RunE: func(cmd *cobra.Command, args []string) error {
        agents := discover.Discover()
        proxySettings = nil
        fmt.Println("\nAI Agent 配置状态:")
        fmt.Println(strings.Repeat("-", 80))

        reg := agent.NewWriterRegistry()
        for _, a := range agents {
            var detail string
            if a.HasConfig && a.IsConfigurable {
                writer := reg.Get(a.Name)
                if writer != nil {
                    _, detail = writer.Status(a.ConfigPath)
                    if detail == "" {
                        detail = "未配置代理"
                    }
                }
            } else if a.HasConfig && !a.IsConfigurable {
                detail = a.Notes
            } else {
                detail = "未安装"
            }

            icon := "❌"
            if a.HasConfig && a.IsConfigured {
                icon = "🔗"
            } else if a.HasConfig {
                icon = "⚙️"
            }

            fmt.Printf("  %-12s %-5s %s %s\n", a.Name, a.Category, icon, detail)
        }
        proxySettings = nil
        fmt.Println()
        return nil
    },
}

// routeCmd shows the model routing table
var routeCmd = &cobra.Command{
    Use:   "route",
    Short: "显示模型路由表",
    RunE: func(cmd *cobra.Command, args []string) error {
        p, err := getProxySettings()
        if err != nil {
            fmt.Printf("未检测到 AI proxy 配置: %v\n", err)
            fmt.Println("（无代理检测，仅显示默认路由）")
            p = &proxy.Proxy{
                BaseURL: "http://127.0.0.1:3688/v1",
                APIKey:  "ccx-dff3eccc518d9830",
                Port:    3688,
                Source:  proxy.ProxyTypeCCX,
                ModelMap: map[string]string{
                    "gpt-5.5": "sensenova-6.7-flash-lite",
                    "gpt-5.4": "deepseek-v4-flash",
                    "opus":    "sensenova-u1-fast",
                    "haiku":   "deepseek-v4-flash",
                },
            }
        }

        fmt.Println("\n模型路由表:")
        fmt.Println(strings.Repeat("-", 70))
        routing := model.BuildRoutingTable(p)
        for _, r := range routing {
            fmt.Printf("  %-10s %-28s → %-28s [%s]\n", r.Agent, r.Model, r.Target, r.Source)
        }
        fmt.Println()
        return nil
    },
}

// sniffCmd sniffs an LLM provider endpoint to detect supported message formats and models.
var (
    sniffURL     string
    sniffKey     string
    sniffVerbose bool
)

var sniffCmd = &cobra.Command{
    Use:   "sniff",
    Short: "嗅探 LLM 提供商的消息格式和可用模型",
    Long: `嗅探指定的 LLM 提供商 endpoint，自动检测其支持的消息格式（OpenAI 兼容协议、Anthropic Messages API 等）
和可用模型列表。

使用方式:
  .\agent-nexus.exe sniff --url https://token.sensenova.cn/v1 --key sk-xxx
  .\agent-nexus.exe sniff --url http://127.0.0.1:8080/v1 --key sk-xxx -v

该命令会依次探测:
  1. /v1/models           获取模型列表
  2. /v1/chat/completions  验证 OpenAI 格式兼容性
  3. /v1/messages          验证 Anthropic Messages API 兼容性

详细模式 (-v):
  显示每个模型的完整信息（字段、能力推断等）
`,
    RunE: func(cmd *cobra.Command, args []string) error {
        if sniffURL == "" || sniffKey == "" {
            return fmt.Errorf("--url 和 --key 均为必选参数")
        }

        fmt.Printf("正在嗅探 LLM endpoint: %s\n\n", sniffURL)

        result, err := sniff.Sniff(sniffURL, sniffKey)
        if err != nil {
            fmt.Printf("嗅探失败: %v\n", err)
            return err
        }

        fmt.Printf("  Endpoint: %s\n", result.BaseURL)

        // Build a multi-format summary line
        var formats []string
        if result.ModelCount > 0 {
            formats = append(formats, "OpenAI models API")
        }
        if result.OpenAICap {
            formats = append(formats, "OpenAI chat completions")
        }
        if result.AnthropicCap {
            formats = append(formats, "Anthropic Messages API")
        }
        if len(formats) > 0 {
            fmt.Printf("  支持格式: %s\n", strings.Join(formats, " / "))
        } else {
            fmt.Printf("  支持格式: 未检测到标准格式\n")
        }

        if result.ModelCount > 0 {
            if sniffVerbose {
                // Detailed mode: show full info per model
                fmt.Printf("\n  可用模型 (%d):\n", result.ModelCount)
                for i, m := range result.Models {
                    if i > 0 {
                        fmt.Println()
                    }
                    fmt.Println(m.FormatVerbose())
                    caps := m.ModelCapabilities()
                    fmt.Printf("    %-40s %s\n", "capabilities:", strings.Join(caps, ", "))
                }
            } else {
                fmt.Printf("\n  可用模型 (%d):\n", result.ModelCount)
                for _, m := range result.Models {
                    fmt.Printf("    - %s\n", m.ID)
                }
            }
        }

        if result.Notes != "" {
            fmt.Printf("\n  备注: %s\n", result.Notes)
        }

        fmt.Println()
        return nil
    },
}

func executeCommand(fullCmd string) error {
    if runtime.GOOS == "windows" {
        fileName := filepath.Base(fullCmd)
        // Handle .ps1 PowerShell scripts - download to temp file, then execute
        // Try pwsh.exe (PowerShell 7) first since kimi/hermes scripts require Get-FileHash
        // which may not work in old Windows PowerShell 5.1 non-interactive sessions
        if strings.HasSuffix(fileName, ".ps1") {
            scriptPath := filepath.Join(os.Getenv("TEMP"), "agent-nexus-install.ps1")

            // Find PowerShell executable: prefer pwsh.exe (PowerShell 7)
            pwshCmd := "pwsh.exe"
            if _, err := exec.LookPath(pwshCmd); err != nil {
                pwshCmd = "powershell.exe"
            }

            // Step 1: Download the script
            dlArgs := []string{
                "-ExecutionPolicy", "Bypass",
                "-Command",
                fmt.Sprintf("irm -UseBasicParsing %q -OutFile %q", fullCmd, scriptPath),
            }
            dlCmd := exec.Command(pwshCmd, dlArgs...)
            dlCmd.Stdout = os.Stdout
            dlCmd.Stderr = os.Stderr
            if err := dlCmd.Run(); err != nil {
                return fmt.Errorf("下载安装脚本失败: %v", err)
            }
            if _, err := os.Stat(scriptPath); err != nil {
                return fmt.Errorf("安装脚本下载失败: %s 未找到", scriptPath)
            }

            // Step 2: Execute the downloaded script
            runCmd := exec.Command(pwshCmd,
                "-ExecutionPolicy", "Bypass",
                "-File", scriptPath)
            runCmd.Stdout = os.Stdout
            runCmd.Stderr = os.Stderr
            if err := runCmd.Run(); err != nil {
                os.Remove(scriptPath)
                return err
            }
            os.Remove(scriptPath)
            return nil
        }
        destPath := filepath.Join(os.Getenv("TEMP"), fileName)
        downloadCmd := fmt.Sprintf("powershell -Command \"Invoke-WebRequest -Uri '%s' -OutFile '%s'\"", fullCmd, destPath)
        cmd := exec.Command("cmd", "/c", downloadCmd)
        cmd.Stdout = os.Stdout
        cmd.Stderr = os.Stderr
        if err := cmd.Run(); err != nil {
            return fmt.Errorf("下载失败: %v", err)
        }
        // 验证文件下载成功
        if _, err := os.Stat(destPath); err != nil {
            return fmt.Errorf("下载失败: 文件未找到 (%s)", destPath)
        }
        execCmd := exec.Command(destPath)
        execCmd.Stdout = os.Stdout
        execCmd.Stderr = os.Stderr
        return execCmd.Run()
    }
    fileName := filepath.Base(fullCmd)
    tmpPath := filepath.Join("/tmp", fileName)
    cmd := exec.Command("sh", "-c", fmt.Sprintf("curl -L -o %s %s", tmpPath, fullCmd))
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("下载失败: %v", err)
    }
    // 验证文件下载成功
    if _, err := os.Stat(tmpPath); err != nil {
        return fmt.Errorf("下载失败: 文件未找到 (%s)", tmpPath)
    }
    execCmd := exec.Command(tmpPath)
    execCmd.Stdout = os.Stdout
    execCmd.Stderr = os.Stderr
    return execCmd.Run()
}


func installAllRuntimes() error {
    agents := install.GetByCategory("cli")
    if len(agents) == 0 {
        fmt.Println("未找到可安装的 CLI agent。")
        return nil
    }
    if installExecute {
        return installAllExecute(agents)
    }
    fmt.Printf("\n正在安装 %d 个 CLI agent 运行时...\n", len(agents))
    fmt.Println(strings.Repeat("-", 60))
    for _, a := range agents {
        cmdStr, _, _ := a.InstallCommand()
        if cmdStr == "" || strings.HasPrefix(cmdStr, "No install") {
            continue
        }
        _, isNpm, isPip := a.InstallCommand()
        if isNpm {
            fmt.Printf("  %-12s  npm install -g %s\n", a.Name, a.NpmPackage)
        } else if isPip {
            fmt.Printf("  %-12s  pip install %s\n", a.Name, a.PipPackage)
        } else {
            fmt.Printf("  %-12s  %s\n", a.Name, cmdStr)
        }
    }
    fmt.Printf("\n依次运行上述命令完成安装，然后运行: agent-nexus discover 确认安装成功\n")
    return nil
}

func installAllExecute(agents []install.Agent) error {
    fmt.Printf("\n正在安装 %d 个 CLI agent 运行时...\n", len(agents))
    fmt.Println(strings.Repeat("-", 60))
    for _, a := range agents {
        cmdStr, isNpm, isPip := a.InstallCommand()
        if cmdStr == "" || strings.HasPrefix(cmdStr, "No install") {
            continue
        }
        fmt.Printf("  正在安装 %s ...", a.Name)
        if isNpm {
            if err := executeNpmCommand(fmt.Sprintf("install -g %s", a.NpmPackage)); err != nil {
                fmt.Printf(" ❌ %v\n", err)
            } else {
                fmt.Println(" ✅")
            }
        } else if isPip {
            if err := executePipCommand(fmt.Sprintf("install %s", a.PipPackage)); err != nil {
                fmt.Printf(" ❌ %v\n", err)
            } else {
                fmt.Println(" ✅")
            }
        } else {
            if err := executeCommand(cmdStr); err != nil {
                fmt.Printf(" ❌ %v\n", err)
            } else {
                fmt.Println(" ✅")
            }
        }
    }
    fmt.Printf("\n安装完成，运行: agent-nexus discover 确认安装成功\n")
    return nil
}

func executeNpmCommand(args string) error {
    cmd := exec.Command("npm", strings.Fields(args)...)
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    return cmd.Run()
}
func executePipCommand(args string) error {
	xmd := exec.Command("pip", strings.Fields(args)...)
	xmd.Stdout = os.Stdout
	xmd.Stderr = os.Stderr
	return xmd.Run()
}




// installCmd is the parent command for installing agent runtimes
var installCmd = &cobra.Command{
    Use:   "install",
    Short: "安装 agent 运行时",
    Long: `安装各种 AI agent 运行时，支持 Windows、Linux、macOS 三个平台。

使用方式:
  agent-nexus install list                # 显示可安装的 agent 列表
  agent-nexus install <name>              # 一键安装指定 agent
  agent-nexus install uninstall <name>    # 卸载指定 agent
  agent-nexus install update <name>       # 更新指定 agent

示例:
  agent-nexus install codex
  agent-nexus install claude
  agent-nexus install --all               # 安装全部 CLI agent
  agent-nexus install --all --execute     # 自动执行安装
  agent-nexus install uninstall codex
  agent-nexus install update codex
`,
    RunE: func(cmd *cobra.Command, args []string) error {
        if installAll {
            return installAllRuntimes()
        }
        if len(args) == 0 {
            return cmd.Usage()
        }
        name := args[0]
        if name == "list" {
            agents := install.AllRuntimes()
            fmt.Printf("\n可安装的 agent 运行时 (%d 个):\n", len(agents))
            fmt.Println(strings.Repeat("-", 80))
            fmt.Printf("  %-10s  %-5s  %-8s  %-35s  %s\n",
                "Name", "Type", "Install", "Display", "Notes")
            fmt.Println(strings.Repeat("-", 80))
            for _, a := range agents {
                cmdStr, _, _ := a.InstallCommand()
                if len(cmdStr) > 35 {
                    cmdStr = cmdStr[:35]
                }
                fmt.Printf("  %-10s  %-5s  %-8s  %-35s  %s\n",
                    a.Name, a.Category, cmdStr, a.Display, a.Notes)
            }
            fmt.Println()
            return nil
        }
        if name == "uninstall" {
            if len(args) < 2 {
                return fmt.Errorf("请指定要卸载的 agent 名称\n\n用法: agent-nexus install uninstall <name>")
            }
            uninstallName := args[1]
            ua := install.GetByName(uninstallName)
            if ua == nil {
                return fmt.Errorf("未知 agent: %s\n\n可用列表: agent-nexus install list", uninstallName)
            }
            uninstCmd, isNpm, isPip := ua.UninstallCommand()
            fmt.Printf("正在卸载 %s (%s)...\n", ua.Display, ua.Name)
            fmt.Println()
            if isNpm {
                fmt.Printf("卸载命令: %s\n", uninstCmd)
                if installExecute {
                    fmt.Println("正在执行...")
                    if err := executeNpmCommand(fmt.Sprintf("uninstall -g %s", ua.NpmPackage)); err != nil {
                        return fmt.Errorf("卸载失败: %v", err)
                    }
                    fmt.Println("✅ 卸载完成")
                } else {
                    fmt.Printf("\n运行以下命令完成卸载:\n  %s\n", uninstCmd)
                    fmt.Printf("\n卸载完成后运行: agent-nexus discover 确认卸载成功\n")
                }
            } else if isPip {
                fmt.Printf("卸载命令: %s\n", uninstCmd)
                if installExecute {
                    fmt.Println("正在执行...")
                    if err := executePipCommand(fmt.Sprintf("uninstall %s", ua.PipPackage)); err != nil {
                        return fmt.Errorf("卸载失败: %v", err)
                    }
                    fmt.Println("✅ 卸载完成")
                } else {
                    fmt.Printf("\n运行以下命令完成卸载:\n  %s\n", uninstCmd)
                }
            } else {
                fmt.Printf("正在卸载 %s (%s)...\n", ua.Display, ua.Name)
                fmt.Println()
                uninstallPaths := ua.GetUninstallPaths()
                legacyBinPaths := ua.GetLegacyBinPaths()
                home, _ := os.UserHomeDir()
                if installExecute {
                    fmt.Println("正在执行卸载...")
                    allRemoved := true
                    // 1. Delete directory paths (e.g. ~/.kimi-code/)
                    for _, rel := range uninstallPaths {
                        full := filepath.Join(home, rel)
                        if _, err := os.Stat(full); err == nil {
                            fmt.Printf("  删除 %s ...", rel)
                            if err := os.RemoveAll(full); err != nil {
                                fmt.Printf(" ❌ %v\n", err)
                                allRemoved = false
                            } else {
                                fmt.Println(" ✅")
                            }
                        } else {
                            fmt.Printf("  %s 未找到（已跳过）\n", rel)
                        }
                    }
                    // 2. Delete individual legacy binary files
                    for _, rel := range legacyBinPaths {
                        full := filepath.Join(home, rel)
                        if _, err := os.Stat(full); err == nil {
                            fmt.Printf("  删除 %s ...", rel)
                            if err := os.Remove(full); err != nil {
                                fmt.Printf(" ❌ %v\n", err)
                                allRemoved = false
                            } else {
                                fmt.Println(" ✅")
                            }
                        } else {
                            fmt.Printf("  %s 未找到（已跳过）\n", rel)
                        }
                    }
                    if allRemoved {
                        fmt.Println("✅ 卸载完成")
                    }
                } else {
                    fmt.Printf("需要删除以下目录/文件:\n")
                    for _, rel := range uninstallPaths {
                        full := filepath.Join(home, rel)
                        fmt.Printf("  %s\n", full)
                    }
                    for _, rel := range legacyBinPaths {
                        full := filepath.Join(home, rel)
                        fmt.Printf("  %s\n", full)
                    }
                    fmt.Printf("\n运行: agent-nexus install uninstall %s --execute 执行删除\n", ua.Name)
                }
            }
            return nil
        }
        if name == "update" {
            if len(args) < 2 {
                return fmt.Errorf("请指定要更新的 agent 名称\n\n用法: agent-nexus install update <name>")
            }
            updateName := args[1]
            ua := install.GetByName(updateName)
            if ua == nil {
                return fmt.Errorf("未知 agent: %s\n\n可用列表: agent-nexus install list", updateName)
            }
            updateCmd, isNpm, isPip := ua.UpdateCommand()
            fmt.Printf("正在更新 %s (%s)...\n", ua.Display, ua.Name)
            fmt.Println()
            if isNpm {
                fmt.Printf("更新命令: %s\n", updateCmd)
                if installExecute {
                    fmt.Println("正在执行...")
                    if err := executeNpmCommand(fmt.Sprintf("install -g %s", ua.NpmPackage)); err != nil {
                        return fmt.Errorf("更新失败: %v", err)
                    }
                    fmt.Println("✅ 更新完成")
                } else {
                    fmt.Printf("\n运行以下命令完成更新:\n  %s\n", updateCmd)
                    fmt.Printf("\n更新完成后运行: agent-nexus discover 确认更新成功\n")
                }
            } else if isPip {
                fmt.Printf("更新命令: %s\n", updateCmd)
                if installExecute {
                    fmt.Println("正在执行...")
                    if err := executePipCommand(fmt.Sprintf("install --upgrade %s", ua.PipPackage)); err != nil {
                        return fmt.Errorf("更新失败: %v", err)
                    }
                    fmt.Println("✅ 更新完成")
                } else {
                    fmt.Printf("\n运行以下命令完成更新:\n  %s\n", updateCmd)
                    fmt.Printf("\n更新完成后运行: agent-nexus discover 确认更新成功\n")
                }
            } else {
                fmt.Printf("更新命令: %s\n", updateCmd)
                if installExecute {
                    fmt.Println("正在执行...")
                    if err := executeCommand(updateCmd); err != nil {
                        return fmt.Errorf("更新失败: %v", err)
                    }
                    fmt.Println("✅ 更新完成")
                } else {
                    fmt.Printf("\n运行以下命令完成更新:\n  %s\n", updateCmd)
                    fmt.Printf("\n更新完成后运行: agent-nexus discover 确认更新成功\n")
                }
            }
            return nil
        }
        a := install.GetByName(name)
        if a == nil {
            return fmt.Errorf("未知 agent: %s\n\n可用列表: agent-nexus install list", name)
        }
        platform := install.CurrentPlatform()
        fmt.Printf("正在安装 %s (%s) 到 %s...\n", a.Display, platform, a.Notes)
        fmt.Println()
        cmdStr, isNpm, isPip := a.InstallCommand()
        if installExecute {
            fmt.Println("正在执行...")
            if isNpm {
                if err := executeNpmCommand(fmt.Sprintf("install -g %s", a.NpmPackage)); err != nil {
                    return fmt.Errorf("安装失败: %v", err)
                }
                fmt.Println("✅ 安装完成")
            } else if isPip {
                if err := executePipCommand(fmt.Sprintf("install %s", a.PipPackage)); err != nil {
                    return fmt.Errorf("安装失败: %v", err)
                }
                fmt.Println("✅ 安装完成")
            } else if cmdStr == "" || strings.HasPrefix(cmdStr, "No install") {
                fmt.Printf("当前平台 (%s) 无可用安装命令\n", platform)
                fmt.Printf("\n所有可用下载地址:\n")
                for p, url := range a.Download {
                    fmt.Printf("  %s: %s\n", p, url)
                }
                if a.NpmPackage != "" {
                    fmt.Printf("\n如果通过 npm 安装: %s\n", "npm install -g " + a.NpmPackage)
                }
                if a.PipPackage != "" {
                    fmt.Printf("\n如果通过 pip 安装: %s\n", "pip install " + a.PipPackage)
                }
            } else {
                if err := executeCommand(cmdStr); err != nil {
                    return fmt.Errorf("安装失败: %v", err)
                }
                fmt.Println("✅ 安装完成")
                // Post-install: try to locate the installed binary and report its path
                home, _ := os.UserHomeDir()
                binPaths := []string{
                    filepath.Join(home, ".kimi-code", "bin", "kimi.exe"),
                    filepath.Join(home, ".kimi-code", "bin", "kimi"),
                    filepath.Join(home, ".local", "bin", "kimi.exe"),
                }
                for _, bp := range binPaths {
                    if _, err := os.Stat(bp); err == nil {
                        fmt.Printf("\n已找到已安装的二进制文件: %s\n", bp)
                        fmt.Printf("请在新的终端中运行: %s\n", filepath.Base(bp))
                        break
                    }
                }
            }
            fmt.Printf("\n安装完成后运行: agent-nexus discover 确认安装成功\n")
        } else {
            if isNpm {
                fmt.Printf("安装命令: %s\n", cmdStr)
                fmt.Printf("\n运行以下命令完成安装:\n  %s\n", cmdStr)
                fmt.Printf("\n安装完成后运行: agent-nexus discover 确认安装成功\n")
                fmt.Printf("\n提示: 使用 --execute 或 -e 标志可直接执行安装\n")
            } else if isPip {
                fmt.Printf("安装命令: %s\n", cmdStr)
                fmt.Printf("\n运行以下命令完成安装:\n  %s\n", cmdStr)
                fmt.Printf("\n安装完成后运行: agent-nexus discover 确认安装成功\n")
                fmt.Printf("\n提示: 使用 --execute 或 -e 标志可直接执行安装\n")
            } else if cmdStr == "" || strings.HasPrefix(cmdStr, "No install") {
                fmt.Printf("当前平台 (%s) 无可用安装命令\n", platform)
                fmt.Printf("\n所有可用下载地址:\n")
                for p, url := range a.Download {
                    fmt.Printf("  %s: %s\n", p, url)
                }
                if a.NpmPackage != "" {
                    fmt.Printf("\n如果通过 npm 安装: %s\n", "npm install -g " + a.NpmPackage)
                }
                if a.PipPackage != "" {
                    fmt.Printf("\n如果通过 pip 安装: %s\n", "pip install " + a.PipPackage)
                }
            } else {
                fmt.Printf("下载地址: %s\n", cmdStr)
                fmt.Printf("\n当前平台 (%s) 的安装方式:\n", platform)
                fmt.Printf("  %s\n", cmdStr)
                fmt.Printf("\n安装完成后运行: agent-nexus discover 确认安装成功\n")
                fmt.Printf("\n提示: 使用 --execute 或 -e 标志可直接执行安装\n")
            }
        }
        return nil
    },
}



func init() {
    // backup flags
    backupCmd.Flags().StringVar(&backupBranch, "branch", "main", "快照所属分支名称")
    backupCmd.Flags().StringVar(&backupMessage, "message", "", "快照提交信息")

    // snapshot flags
    snapshotCmd.Flags().StringVar(&snapshotBranch, "branch", "main", "快照所属分支名称")
    snapshotCmd.Flags().StringVar(&snapshotMessage, "message", "", "快照提交信息")

    // restore flags
    restoreCmd.Flags().StringVar(&restoreID, "snapshot", "", "要恢复到的快照 ID（输入 'latest' 恢复最新快照）")
    restoreCmd.MarkFlagRequired("snapshot")

    // configure flags
    configureCmd.Flags().StringVar(&configureAgents, "agents", "", "要配置的 agent 名称（逗号分隔），使用 all 配置所有已安装的 agent（必选）")
    configureCmd.Flags().StringVar(&configureModels, "models", "", "可选：用 模型名 覆盖默认映射，格式: agent=模型名,agent2=模型名2，如 codex=gpt-5.5,claude=deepseek-v4")
    configureCmd.MarkFlagRequired("agents")

    // diff flags
    diffCmd.Flags().StringVar(&diffOld, "old", "", "旧快照 ID（或 'latest'）")
    diffCmd.Flags().StringVar(&diffNew, "new", "", "新快照 ID（或 'latest'）")
    diffCmd.MarkFlagRequired("old")
    diffCmd.MarkFlagRequired("new")

    // discover flags
    discoverCmd.Flags().BoolVarP(&discoverVerbose, "verbose", "v", false, "显示 agent 支持的所有模型及模型来源（自定义 vs. 模型重定义）")

    // branch flags
    branchCmd.Flags().StringVar(&branchCreateName, "create", "", "创建新分支名称")
    branchCmd.Flags().StringVar(&branchSwitchName, "switch", "", "切换到指定分支")
    branchCmd.Flags().BoolVar(&branchShow, "show", false, "显示当前分支信息")

    // sniff flags
    sniffCmd.Flags().StringVar(&sniffURL, "url", "", "LLM provider endpoint URL（必选）")
    sniffCmd.Flags().StringVar(&sniffKey, "key", "", "LLM provider API key（必选）")
    sniffCmd.MarkFlagRequired("url")
    sniffCmd.MarkFlagRequired("key")
    sniffCmd.Flags().BoolVarP(&sniffVerbose, "verbose", "v", false, "显示每个模型的详细信息")

    rootCmd.AddCommand(discoverCmd)
    rootCmd.AddCommand(detectCmd)
    rootCmd.AddCommand(backupCmd)
    rootCmd.AddCommand(configureCmd)
    rootCmd.AddCommand(statusCmd)
    rootCmd.AddCommand(routeCmd)
    rootCmd.AddCommand(snapshotCmd)
    rootCmd.AddCommand(restoreCmd)
    rootCmd.AddCommand(versionCmd)
    rootCmd.AddCommand(diffCmd)
    rootCmd.AddCommand(branchCmd)
    rootCmd.AddCommand(sniffCmd)
    rootCmd.AddCommand(installCmd)
    installCmd.Flags().BoolVarP(&installAll, "all", "a", false, "安装全部 CLI agent")
    installCmd.Flags().BoolVarP(&installExecute, "execute", "e", true, "直接执行安装命令")
    installCmd.Flags().BoolVar(&installForce, "force", false, "强制安装")
}

// Execute runs the root command
func Execute() {
    if err := rootCmd.Execute(); err != nil {
        os.Exit(1)
    }
}






