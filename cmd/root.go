package cmd

import (
    "fmt"
    "os"
    "path/filepath"
    "sort"
    "strings"

    "github.com/spf13/cobra"
    "agent-nexus/internal/agent"
    "agent-nexus/internal/backup"
    "agent-nexus/internal/discover"
    "agent-nexus/internal/model"
    "agent-nexus/internal/proxy"
)

var rootCmd = &cobra.Command{
    Use:   "agent-nexus",
    Short: "Go AI Agent Configuration Tool - 自动化配置各种 AI coding agent",
    Long: `agent-nexus - 一键自动化配置各种 AI coding agent

功能：
  1. 自动发现本机已安装的 AI agent (codex, claude, kimi, deepseek, opencode 等)
  2. 自动检测 CCX Desktop 代理配置 (URL, Key, 模型映射)
  3. 配置生效前自动备份原有配置
  4. 将 CCX 代理配置写入各 agent 配置文件
  5. 自动模型重定向，匹配最佳后端模型

支持的 agent:
  CLI:  codex, claude, kimi, deepseek, opencode, openclaw,
        codebuddy, hermes, kiro, grok, qoder, trae
  IDE:  cursor (via openai-compatible provider)
  不可配置: antigravity, copilot, deveco, pi, qoder-ide, trae-ide,
            codebuddy-ide, windsurf, zed

用法：
  agent-nexus discover   扫描并列出已安装的 agent
  agent-nexus detect     检测 CCX Desktop 代理配置
  agent-nexus backup     备份所有配置
  agent-nexus configure  备份后自动配置指定的 agent（必选 --agents 参数）
  agent-nexus status     显示配置状态
  agent-nexus route      显示模型路由表
`,
}

var proxySettings *proxy.Proxy

func init() {
    rootCmd.PersistentFlags().String("home", "", "user home directory (auto-detected by default)")
    rootCmd.PersistentFlags().StringVar(&proxyURL, "url", "", "directly specify proxy URL (overrides auto-detect)")
    rootCmd.PersistentFlags().StringVar(&proxyKey, "key", "", "directly specify proxy API key (overrides auto-detect)")
}

var proxyURL string
var proxyKey string

func getProxySettings() (*proxy.Proxy, error) {
    if proxyURL != "" || proxyKey != "" {
        return proxy.FromFlags(proxyURL, proxyKey)
    }
    return proxy.Detect()
}

// discoverCmd scans for installed agents
var discoverCmd = &cobra.Command{
    Use:   "discover",
    Short: "扫描并列出已安装的 AI agent",
    RunE: func(cmd *cobra.Command, args []string) error {
        agents := discover.Discover()
        fmt.Printf("\n已发现 %d 个 AI agent:\n", len(agents))
        fmt.Println(strings.Repeat("-", 80))

        for _, a := range agents {
            statusIcon := "❌"
            if a.HasConfig {
                statusIcon = "✅"
            }
            notes := ""
            if !a.IsConfigurable && a.Notes != "" {
                notes = fmt.Sprintf("            └─ ⚠ %s\n", a.Notes)
            }
            if a.HasConfig && a.IsConfigured {
                notes = "            └─ 🔗 已配置代理\n"
            }
            fmt.Printf("  %-12s %-5s [%s] %s\n%s", a.Name, a.Category, statusIcon, a.ConfigPath, notes)
        }
        fmt.Println()
        return nil
    },
}

// detectCmd auto-detects CCX Desktop proxy settings
var detectCmd = &cobra.Command{
    Use:   "detect",
    Short: "检测 CCX Desktop 代理配置 (URL, Key, 模型映射)",
    RunE: func(cmd *cobra.Command, args []string) error {
        p, err := getProxySettings()
        if err != nil {
            fmt.Printf("未能检测到 CCX Desktop 配置: %v\n", err)
            return err
        }
        fmt.Printf("\nCCX Desktop 代理配置已检测:\n")
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

// backupCmd backs up all agent configs
var backupCmd = &cobra.Command{
    Use:   "backup",
    Short: "备份所有 agent 配置文件",
    RunE: func(cmd *cobra.Command, args []string) error {
        home, _ := os.UserHomeDir()
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

        results, err := backup.Backup(paths, filepath.Dir(destRoot))
        if err != nil {
            fmt.Printf("备份失败: %v\n", err)
            return err
        }

        success := 0
        for _, r := range results {
            if r.Success {
                fmt.Printf("  ✅ %s → %s\n", filepath.Base(r.Source), r.Dest)
                success++
            } else {
                fmt.Printf("  ❌ %s: %s\n", filepath.Base(r.Source), r.Error)
            }
        }
        fmt.Printf("\n备份完成: %d/%d 成功\n", success, len(results))
        return nil
    },
}

// configureCmd backs up then configures selected agents (required --agents flag)
var (
    configureAgents string
)

var configureCmd = &cobra.Command{
    Use:   "configure",
    Short: "备份后自动配置指定的 agent（必选 --agents 参数）",
    Long: `agent-nexus configure --agents <agent1[,agent2,...]|all>

必选参数:
  --agents  要配置的 agent 名称（逗号分隔）或 all 表示配置所有已安装的 agent

示例:
  agent-nexus configure --agents all              # 配置所有已安装的 agent
  agent-nexus configure --agents claude,kimi      # 仅配置 Claude 和 Kimi
  agent-nexus configure --agents codex             # 仅配置 Codex
`,
    RunE: func(cmd *cobra.Command, args []string) error {
        if configureAgents == "" {
            return fmt.Errorf("--agents 为必选参数，请指定要配置的 agent（使用 all 配置所有）")
        }

        fmt.Println("[1/4] 检测 CCX Desktop 代理...")
        p, err := getProxySettings()
        if err != nil {
            proxySettings = p
            fmt.Printf("❌ 未检测到 CCX Desktop 配置: %v\n", err)
            fmt.Println("   请确保 CCX Desktop 已安装并运行（监听 127.0.0.1:3688）")
            return err
        }
        proxySettings = p
        fmt.Printf("  ✅ 代理: %s  密钥: %s\n", p.BaseURL, p.APIKey)
        fmt.Println()

        fmt.Println("[2/4] 扫描已安装的 agent...")
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
        var configuredNames []string
        for _, a := range agents {
            nameToAgent[a.Name] = a
            if a.HasConfig && a.IsConfigurable {
                configuredNames = append(configuredNames, a.Name)
            }
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

        fmt.Println("[3/4] 备份现有配置...")
        home, _ := os.UserHomeDir()
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
                for _, r := range results {
                    if r.Success {
                        fmt.Printf("  ✅ %s\n", filepath.Base(r.Source))
                    }
                }
            }
        }
        fmt.Println()

        fmt.Println("[4/4] 配置 agent...")
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

            err := writer.Configure(a.ConfigPath, p)
            if err != nil {
                fmt.Printf("  ❌ %s: %v\n", a.Name, err)
                skipped++
            } else {
                _, status := writer.Status(a.ConfigPath)
                fmt.Printf("  ✅ %s → %s\n", a.Name, status)
                configured++
            }
        }

        fmt.Printf("\n配置完成: %d 个 agent 已配置, %d 个跳过\n", configured, skipped)

        fmt.Println("\n模型路由表:")
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
            fmt.Printf("未检测到 CCX Desktop 配置: %v\n", err)
            fmt.Println("（无代理检测，仅显示默认路由）")
            p = &proxy.Proxy{
                BaseURL: "http://127.0.0.1:3688/v1",
                APIKey:  "ccx-dff3eccc518d9830",
                Port:    3688,
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

func init() {
    configureCmd.Flags().StringVar(&configureAgents, "agents", "", "要配置的 agent 名称（逗号分隔），使用 all 配置所有已安装的 agent（必选）")
    configureCmd.MarkFlagRequired("agents")
    rootCmd.AddCommand(discoverCmd)
    rootCmd.AddCommand(detectCmd)
    rootCmd.AddCommand(backupCmd)
    rootCmd.AddCommand(configureCmd)
    rootCmd.AddCommand(statusCmd)
    rootCmd.AddCommand(routeCmd)
}

// Execute runs the root command
func Execute() {
    if err := rootCmd.Execute(); err != nil {
        os.Exit(1)
    }
}
