package cmd

import (
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "runtime"
    "strings"
    "time"

    "github.com/spf13/cobra"
    "agent-nexus/internal/db"
    "agent-nexus/internal/discover"
    "agent-nexus/internal/install"
    "agent-nexus/internal/model"
    "agent-nexus/internal/proxy"
    "agent-nexus/internal/sniff"
    "agent-nexus/internal/versioning"
)

var homeDir string
var proxyURL string
var proxyKey string

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
        codebuddy, hermes, kiro, grok, qoder, trae, pi
  IDE:  cursor (via openai-compatible provider)
  不可配置: antigravity, copilot, Deveco, qoder-ide, trae-ide,
            codebuddy-ide, windsurf, zed

用法：
  agent-nexus agent discover [-v]   扫描已安装的 agent（-v 显示支持模型）
  agent-nexus agent list            显示可安装的 agent 列表
  agent-nexus agent install <name>  安装 agent 运行时
  agent-nexus agent uninstall <name> 卸载指定 agent
  agent-nexus agent update <name>   更新指定 agent
  agent-nexus proxy detect          检测 AI 代理配置
  agent-nexus proxy route           显示模型路由表
  agent-nexus proxy sniff           嗅探 LLM 提供商消息格式与模型
  agent-nexus proxy db add          嗅探并保存代理配置到数据库
  agent-nexus proxy db query        查询代理配置（可选按 ID 或 URL 过滤）
  agent-nexus conf bak              备份所有配置（自动版本化）
  agent-nexus conf history          列出所有配置快照
  agent-nexus conf show             创建配置快照
  agent-nexus conf rollback -s <id> 恢复到指定快照
  agent-nexus conf diff --old --new 对比两个快照的差异
  agent-nexus conf branch           管理配置分支

全局选项：
  --home string   指定用户目录
  --url string    直接指定代理 URL（覆盖自动检测）
  --key string    直接指定代理 API key（覆盖自动检测）
`,
}

func init() {
    rootCmd.PersistentFlags().StringVar(&homeDir, "home", "", "user home directory (auto-detected by default)")
    rootCmd.PersistentFlags().StringVar(&proxyURL, "url", "", "directly specify proxy URL (overrides auto-detect)")
    rootCmd.PersistentFlags().StringVar(&proxyKey, "key", "", "directly specify proxy API key (overrides auto-detect)")

    rootCmd.AddCommand(agentCmd)
    rootCmd.AddCommand(proxyCmd)
    rootCmd.AddCommand(confCmd)
}

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

// ========== AGENT GROUP ==========

var agentCmd = &cobra.Command{
    Use:   "agent",
    Short: "Agent 管理（发现、安装、卸载、更新）",
    Long: `Agent 管理命令组，用于发现本机已安装的 AI agent、安装/卸载/更新 agent 运行时。

子命令：
  discover  扫描已安装的 agent
  list      显示可安装的 agent 列表
  install   安装指定 agent
  uninstall 卸载指定 agent
  update    更新指定 agent
`,
}

var discoverVerbose bool

var agentDiscoverCmd = &cobra.Command{
    Use:   "discover [-v]",
    Short: "扫描并列出已安装的 AI agent",
    Long: `扫描本机已安装的 AI coding agent（codex, claude, kimi, deepseek, opencode 等），显示配置状态。
使用 -v 可显示每个 agent 支持的模型及模型来源（自定义 vs. 模型重定义）。`,
    RunE: func(cmd *cobra.Command, args []string) error {
        agents := discover.Discover()
        discover.RenderTable(agents)

        if discoverVerbose {
            fmt.Printf("正在检测 AI 代理以获取模型信息...")
            p, err := getProxySettings()
            if err != nil {
                fmt.Printf("  未检测到 AI 代理配置（将仅显示默认模型）\n")
            } else {
                fmt.Printf("  代理: %s (%s)\n", p.Source, p.BaseURL)
            }

            fmt.Printf("\n模型支持详情:\n")
            discover.RenderVerboseTable(agents)

            routing := model.BuildRoutingTable(p)
            fmt.Println("模型路由表:")
            fmt.Println(strings.Repeat("-", 70))
            for _, r := range routing {
                fmt.Printf("  %-10s %-28s → %-28s [%s]\n", r.Agent, r.Model, r.Target, r.Source)
            }
            fmt.Println()
        }

        return nil
    },
}

var agentListCmd = &cobra.Command{
    Use:   "list",
    Short: "显示可安装的 agent 运行时列表",
    RunE: func(cmd *cobra.Command, args []string) error {
        agents := install.AllRuntimes()
        fmt.Printf("\n可安装的 agent 运行时 (%d 个):\n", len(agents))
        fmt.Println(strings.Repeat("-", 80))
        fmt.Printf("  %-10s  %-5s  %-8s  %-35s  %s\n", "Name", "Type", "Install", "Display", "Notes")
        fmt.Println(strings.Repeat("-", 80))
        for _, a := range agents {
            cmdStr, _, _ := a.InstallCommand()
            if len(cmdStr) > 35 {
                cmdStr = cmdStr[:35]
            }
            fmt.Printf("  %-10s  %-5s  %-8s  %-35s  %s\n", a.Name, a.Category, cmdStr, a.Display, a.Notes)
        }
        fmt.Println()
        return nil
    },
}

var installAll bool
var installExecute bool = true
var installForce bool

var agentInstallCmd = &cobra.Command{
    Use:   "install <name>",
    Short: "安装 agent 运行时",
    Long: `安装指定的 AI agent 运行时，支持 Windows、Linux、macOS。

用法：
  agent-nexus agent install codex     安装 codex
  agent-nexus agent install claude    安装 claude
  agent-nexus agent install --all     安装全部 CLI agent
  agent-nexus agent install --all --execute  自动执行安装
  agent-nexus agent list              查看可安装的 agent 列表

选项：
  --all, -a           安装全部 CLI agent
  --execute, -e       直接执行安装命令（默认启用）
  --force             强制安装
`,
    RunE: func(cmd *cobra.Command, args []string) error {
        if installAll {
            return installAllRuntimes()
        }
        if len(args) == 0 {
            return cmd.Usage()
        }
        name := args[0]
        a := install.GetByName(name)
        if a == nil {
            return fmt.Errorf("未知 agent: %s\n\n可用列表: agent-nexus agent list", name)
        }
        platform := install.CurrentPlatform()
        fmt.Printf("正在安装 %s (%s) 到 %s...\n", a.Display, platform, a.Notes)
        fmt.Println()
        cmdStr, isNpm, isPip := a.InstallCommand()
        if installExecute {
            fmt.Println("正在执行...")
            if isNpm {
                if err := executeNpmCommand(fmt.Sprintf("install -g %s", a.NpmPackage), installForce); err != nil {
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

                home, _ := os.UserHomeDir()
                binPaths := []string{
                    filepath.Join(home, "."+name+"-code", "bin", name+".exe"),
                    filepath.Join(home, "."+name+"-code", "bin", name),
                    filepath.Join(home, "."+name, "bin", name+".exe"),
                    filepath.Join(home, "."+name, "bin", name),
                    filepath.Join(home, ".local", "bin", name+".exe"),
                    filepath.Join(home, ".local", "bin", name),
                    filepath.Join(home, "AppData", "Local", name, "bin", name+".exe"),
                    filepath.Join(home, "AppData", "Local", name+"-code", "bin", name+".exe"),
                }
                found := false
                for _, bp := range binPaths {
                    if _, err := os.Stat(bp); err == nil {
                        fmt.Printf("\n已找到已安装的二进制文件: %s\n", bp)
                        fmt.Printf("请在新的终端中运行: %s\n", filepath.Base(bp))
                        found = true
                        break
                    }
                }
                if !found {
                    binName := name
                    if runtime.GOOS == "windows" && !strings.HasSuffix(binName, ".exe") {
                        binName = name + ".exe"
                    }
                    if binPath, err := exec.LookPath(binName); err == nil {
                        fmt.Printf("\n已找到已安装的二进制文件: %s\n", binPath)
                        fmt.Printf("请在新的终端中运行: %s\n", filepath.Base(binPath))
                        found = true
                    }
                }
                if !found {
                    fmt.Printf("\n未找到已安装的二进制文件，请检查安装日志或手动查找\n")
                    fmt.Printf("常见位置: ~/.%s-code/bin/ 或 ~/.%s/bin/\n", name, name)
                }
            }
            fmt.Printf("\n安装完成后运行: agent-nexus agent discover 确认安装成功\n")
        } else {
            if isNpm {
                fmt.Printf("安装命令: %s\n", cmdStr)
                fmt.Printf("\n运行以下命令完成安装:\n  %s\n", cmdStr)
                fmt.Printf("\n安装完成后运行: agent-nexus agent discover 确认安装成功\n")
                fmt.Printf("\n提示: 使用 --execute 或 -e 标志可直接执行安装\n")
            } else if isPip {
                fmt.Printf("安装命令: %s\n", cmdStr)
                fmt.Printf("\n运行以下命令完成安装:\n  %s\n", cmdStr)
                fmt.Printf("\n安装完成后运行: agent-nexus agent discover 确认安装成功\n")
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
                fmt.Printf("\n安装完成后运行: agent-nexus agent discover 确认安装成功\n")
                fmt.Printf("\n提示: 使用 --execute 或 -e 标志可直接执行安装\n")
            }
        }
        return nil
    },
}

var agentUninstallCmd = &cobra.Command{
    Use:   "uninstall <name>",
    Short: "卸载 agent 运行时",
    Long: `卸载指定的 AI agent 运行时。

用法：
  agent-nexus agent uninstall codex
  agent-nexus agent uninstall claude
  agent-nexus agent uninstall codex --execute  直接执行卸载
`,
    RunE: func(cmd *cobra.Command, args []string) error {
        if len(args) < 1 {
            return fmt.Errorf("请指定要卸载的 agent 名称\n\n用法: agent-nexus agent uninstall <name>")
        }
        name := args[0]
        ua := install.GetByName(name)
        if ua == nil {
            return fmt.Errorf("未知 agent: %s\n\n可用列表: agent-nexus agent list", name)
        }
        uninstCmd, isNpm, isPip := ua.UninstallCommand()
        fmt.Printf("正在卸载 %s (%s)...\n", ua.Display, ua.Name)
        fmt.Println()
        if isNpm {
            fmt.Printf("卸载命令: %s\n", uninstCmd)
            if installExecute {
                fmt.Println("正在执行...")
                if err := executeNpmCommand(fmt.Sprintf("uninstall -g %s", ua.NpmPackage), installForce); err != nil {
                    return fmt.Errorf("卸载失败: %v", err)
                }
                fmt.Println("✅ 卸载完成")
            } else {
                fmt.Printf("\n运行以下命令完成卸载:\n  %s\n", uninstCmd)
                fmt.Printf("\n卸载完成后运行: agent-nexus agent discover 确认卸载成功\n")
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
                fmt.Printf("\n运行: agent-nexus agent uninstall %s --execute 执行删除\n", ua.Name)
            }
        }
        return nil
    },
}

var agentUpdateCmd = &cobra.Command{
    Use:   "update <name>",
    Short: "更新 agent 运行时",
    Long: `更新指定的 AI agent 运行时到最新版本。

用法：
  agent-nexus agent update codex
  agent-nexus agent update claude
  agent-nexus agent update codex --execute  直接执行更新
`,
    RunE: func(cmd *cobra.Command, args []string) error {
        if len(args) < 1 {
            return fmt.Errorf("请指定要更新的 agent 名称\n\n用法: agent-nexus agent update <name>")
        }
        name := args[0]
        ua := install.GetByName(name)
        if ua == nil {
            return fmt.Errorf("未知 agent: %s\n\n可用列表: agent-nexus agent list", name)
        }
        updateCmd, isNpm, isPip := ua.UpdateCommand()
        fmt.Printf("正在更新 %s (%s)...\n", ua.Display, ua.Name)
        fmt.Println()
        if isNpm {
            fmt.Printf("更新命令: %s\n", updateCmd)
            if installExecute {
                fmt.Println("正在执行...")
                if err := executeNpmCommand(fmt.Sprintf("install -g %s", ua.NpmPackage), installForce); err != nil {
                    return fmt.Errorf("更新失败: %v", err)
                }
                fmt.Println("✅ 更新完成")
            } else {
                fmt.Printf("\n运行以下命令完成更新:\n  %s\n", updateCmd)
                fmt.Printf("\n更新完成后运行: agent-nexus agent discover 确认更新成功\n")
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
                fmt.Printf("\n更新完成后运行: agent-nexus agent discover 确认更新成功\n")
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
                fmt.Printf("\n更新完成后运行: agent-nexus agent discover 确认更新成功\n")
            }
        }
        return nil
    },
}

func initAgentCmd() {
    agentDiscoverCmd.Flags().BoolVarP(&discoverVerbose, "verbose", "v", false, "显示 agent 支持的所有模型及模型来源（自定义 vs. 模型重定义）")
    agentInstallCmd.Flags().BoolVarP(&installAll, "all", "a", false, "安装全部 CLI agent")
    agentInstallCmd.Flags().BoolVarP(&installExecute, "execute", "e", true, "直接执行安装命令")
    agentInstallCmd.Flags().BoolVar(&installForce, "force", false, "强制安装")

    agentCmd.AddCommand(agentDiscoverCmd)
    agentCmd.AddCommand(agentListCmd)
    agentCmd.AddCommand(agentInstallCmd)
    agentCmd.AddCommand(agentUninstallCmd)
    agentCmd.AddCommand(agentUpdateCmd)
}

// ========== PROXY GROUP ==========

var proxyCmd = &cobra.Command{
    Use:   "proxy",
    Short: "AI 消息网关管理（检测、路由、嗅探）",
    Long: `代理管理命令组，用于检测 AI 代理配置、显示模型路由表、嗅探 LLM 提供商。

子命令：
  detect    检测 AI 代理配置
  route     显示模型路由表
  sniff     嗅探 LLM 提供商消息格式与模型
  db        管理已嗅探的代理配置数据库
`,
}

var proxyDetectCmd = &cobra.Command{
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
        _ = p.Port
        fmt.Printf("  密钥:   %s\n", p.APIKey)
        fmt.Printf("\n  模型映射表 (%d 条):\n", len(p.ModelMap))
        for src, dst := range p.ModelMap {
            fmt.Printf("    %-15s → %s\n", src, dst)
        }
        fmt.Println()
        return nil
    },
}

var proxyRouteCmd = &cobra.Command{
    Use:   "route",
    Short: "显示模型路由表",
    RunE: func(cmd *cobra.Command, args []string) error {
        p, err := getProxySettings()
        if err != nil {
            fmt.Printf("未能检测到 AI 代理配置: %v\n", err)
            return err
        }
        _ = discover.Discover()
        routing := model.BuildRoutingTable(p)
        fmt.Println("模型路由表:")
        fmt.Println(strings.Repeat("-", 70))
        for _, r := range routing {
            fmt.Printf("  %-10s %-28s → %-28s [%s]\n", r.Agent, r.Model, r.Target, r.Source)
        }
        fmt.Println()
        return nil
    },
}

var sniffURL string
var sniffKey string
var sniffVerbose bool

var proxySniffCmd = &cobra.Command{
    Use:   "sniff",
    Short: "嗅探 LLM 提供商的消息格式和可用模型",
    Long: `嗅探 LLM 提供商的 endpoint，自动检测其支持的消息格式和可用模型列表。

用法：
  agent-nexus proxy sniff -u https://api.example.com/v1 -k sk-xxx
  agent-nexus proxy sniff -u http://127.0.0.1:3688/v1 -k key123 -v
`,
    RunE: func(cmd *cobra.Command, args []string) error {
        result, err := sniff.Sniff(sniffURL, sniffKey)
        if err != nil {
            fmt.Printf("嗅探失败: %v\n", err)
            return err
        }
        fmt.Printf("\n嗅探结果: %s\n", result.BaseURL)
        fmt.Printf("  检测格式: %s\n", result.DetectedFormat)
        fmt.Printf("  OpenAI 兼容: %v\n", result.OpenAICap)
        fmt.Printf("  Anthropic 兼容: %v\n", result.AnthropicCap)
        fmt.Printf("  模型数量: %d\n", result.ModelCount)
        fmt.Printf("  备注: %s\n", result.Notes)
        if sniffVerbose {
            fmt.Printf("\n  模型列表 (%d):\n", len(result.Models))
            for i, m := range result.Models {
                fmt.Printf("  %3d. %s\n", i+1, m.ID)
            }
        }
        fmt.Println()
        return nil
    },
}

var proxyDbCmd = &cobra.Command{
    Use:   "db",
    Short: "管理已嗅探的代理配置数据库（嵌入式 SQLite）",
    Long: `管理已嗅探的代理配置数据库。

子命令：
  add     嗅探代理并保存到数据库
  list    列出已保存的代理配置
  show    显示指定代理配置详情
  rm      删除指定代理配置
  query   查询代理配置（可选按 ID 或 URL 过滤）
`,
}

var proxyDbAddCmd = &cobra.Command{
    Use:   "add",
    Short: "嗅探代理并保存到数据库",
    Long: `嗅探指定的 LLM 代理 endpoint，如果成功则自动保存到嵌入式 SQLite 数据库中。

用法：
  agent-nexus proxy db add -u https://api.example.com/v1 -k sk-xxx
`,
    RunE: func(cmd *cobra.Command, args []string) error {
        result, err := sniff.Sniff(sniffURL, sniffKey)
        if err != nil {
            fmt.Printf("嗅探失败: %v\n", err)
            return err
        }

        dbPath := filepath.Join(userHomeDir(), ".agent-nexus", "proxies.db")
        dir := filepath.Dir(dbPath)
        if err := os.MkdirAll(dir, 0o755); err != nil {
            return fmt.Errorf("创建数据库目录失败: %v", err)
        }

        db, err := db.New()
        if err != nil {
            return fmt.Errorf("打开数据库失败: %v", err)
        }
        if err := db.Init(); err != nil {
            return fmt.Errorf("初始化数据库失败: %v", err)
        }

        modelIDs := make([]string, 0, len(result.Models))
        for _, m := range result.Models {
            modelIDs = append(modelIDs, m.ID)
        }

        if err := db.Add(result.BaseURL, sniffKey, result.DetectedFormat, result.OpenAICap, result.AnthropicCap, result.ModelCount, modelIDs, time.Now()); err != nil {
            fmt.Printf("保存到数据库失败: %v\n", err)
            return err
        }

        fmt.Printf("\n✅ 已保存到数据库: %s\n", result.BaseURL)
        fmt.Printf("  检测格式: %s\n", result.DetectedFormat)
        fmt.Printf("  模型数量: %d\n", result.ModelCount)
        fmt.Printf("  时间: %s\n", time.Now().Format("2006-01-02 15:04:05"))
        fmt.Println()
        return nil
    },
}

var proxyDbListCmd = &cobra.Command{
    Use:   "list",
    Short: "列出已保存的代理配置",
    RunE: func(cmd *cobra.Command, args []string) error {
        dbPath := filepath.Join(userHomeDir(), ".agent-nexus", "proxies.db")
        if _, err := os.Stat(dbPath); os.IsNotExist(err) {
            fmt.Println("数据库为空，没有已保存的代理配置。")
            return nil
        }
        db, err := db.New()
        if err != nil {
            return fmt.Errorf("打开数据库失败: %v", err)
        }
        if err := db.Init(); err != nil {
            return fmt.Errorf("初始化数据库失败: %v", err)
        }
        records, err := db.List()
        if err != nil {
            return fmt.Errorf("读取数据库失败: %v", err)
        }
        if len(records) == 0 {
            fmt.Println("数据库为空，没有已保存的代理配置。")
            return nil
        }
        fmt.Printf("\n已保存的代理配置 (%d 条):\n", len(records))
        fmt.Println(strings.Repeat("-", 80))
        fmt.Printf("  %-6s  %-45s  %-30s  %s\n", "ID", "URL", "检测格式", "时间")
        fmt.Println(strings.Repeat("-", 80))
        for _, r := range records {
            fmt.Printf("  %-6d  %-45s  %-30s  %s\n", r.ID, r.URL, r.DetectedFormat, r.CreatedAt.Format("2006-01-02 15:04:05"))
        }
        fmt.Println()
        return nil
    },
}

var proxyDbShowCmd = &cobra.Command{
    Use:   "show <id>",
    Short: "显示指定代理配置详情",
    RunE: func(cmd *cobra.Command, args []string) error {
        if len(args) < 1 {
            return fmt.Errorf("请指定代理配置 ID\n\n用法: agent-nexus proxy db show <id>")
        }
        db, err := db.New()
        if err != nil {
            return fmt.Errorf("打开数据库失败: %v", err)
        }
        if err := db.Init(); err != nil {
            return fmt.Errorf("初始化数据库失败: %v", err)
        }
        record, err := db.GetByID(parseInt(args[0]))
        if err != nil {
            fmt.Printf("查询失败: %v\n", err)
            return err
        }
        if record == nil {
            fmt.Printf("未找到 ID 为 %s 的代理配置\n", args[0])
            return nil
        }
        fmt.Printf("\n代理配置详情:\n")
        fmt.Printf("  ID:        %d\n", record.ID)
        fmt.Printf("  URL:       %s\n", record.URL)
        fmt.Printf("  检测格式:  %s\n", record.DetectedFormat)
        fmt.Printf("  OpenAI:    %v\n", record.OpenAICap)
        fmt.Printf("  Anthropic: %v\n", record.AnthropicCap)
        fmt.Printf("  模型数量:  %d\n", record.ModelCount)
        fmt.Printf("  时间:      %s\n", record.CreatedAt.Format("2006-01-02 15:04:05"))
        fmt.Println()
        return nil
    },
}

var proxyDbRmCmd = &cobra.Command{
    Use:   "rm <id>",
    Short: "删除指定代理配置",
    RunE: func(cmd *cobra.Command, args []string) error {
        if len(args) < 1 {
            return fmt.Errorf("请指定代理配置 ID\n\n用法: agent-nexus proxy db rm <id>")
        }
        db, err := db.New()
        if err != nil {
            return fmt.Errorf("打开数据库失败: %v", err)
        }
        if err := db.Init(); err != nil {
            return fmt.Errorf("初始化数据库失败: %v", err)
        }
        if err := db.Delete(parseInt(args[0])); err != nil {
            fmt.Printf("删除失败: %v\n", err)
            return err
        }
        fmt.Printf("✅ 已删除 ID 为 %s 的代理配置\n", args[0])
        return nil
    },
}

        
var proxyDbQueryCmd = &cobra.Command{
    Use:   "query [filter]",
    Short: "查询代理配置（可选按 ID 或 URL 过滤）",
    Long: `查询已保存的代理配置记录。可指定过滤条件：
  - 数字 ID：按 ID 精确查询
  - 字符串：按 URL 子串模糊查询
  - 空：列出所有记录

用法：
  agent-nexus proxy db query
  agent-nexus proxy db query 1
  agent-nexus proxy db query example.com
`,
    RunE: func(cmd *cobra.Command, args []string) error {
        dbPath := filepath.Join(userHomeDir(), ".agent-nexus", "proxies.db")
        if _, err := os.Stat(dbPath); os.IsNotExist(err) {
            fmt.Println("数据库为空，没有已保存的代理配置。")
            return nil
        }
        db, err := db.New()
        if err != nil {
            return fmt.Errorf("打开数据库失败: %v", err)
        }
        if err := db.Init(); err != nil {
            return fmt.Errorf("初始化数据库失败: %v", err)
        }

        filter := ""
        if len(args) > 0 {
            filter = args[0]
        }
        records, err := db.Query(filter)
        if err != nil {
            return fmt.Errorf("查询失败: %v", err)
        }
        if len(records) == 0 {
            if filter != "" {
                fmt.Printf("未找到匹配 '%s' 的代理配置。\n", filter)
            } else {
                fmt.Println("数据库为空，没有已保存的代理配置。")
            }
            return nil
        }

        if filter != "" {
            fmt.Printf("\n查询结果（过滤条件: %s）(%d 条):\n", filter, len(records))
        } else {
            fmt.Printf("\n已保存的代理配置 (%d 条):\n", len(records))
        }
        fmt.Println(strings.Repeat("-", 80))
        fmt.Printf("  %-6s  %-45s  %-30s  %s\n", "ID", "URL", "检测格式", "时间")
        fmt.Println(strings.Repeat("-", 80))
        for _, r := range records {
            fmt.Printf("  %-6d  %-45s  %-30s  %s\n", r.ID, r.URL, r.DetectedFormat, r.CreatedAt.Format("2006-01-02 15:04:05"))
        }
        fmt.Println()
        return nil
    },
}


func initProxyCmd() {
    proxySniffCmd.Flags().StringVar(&sniffURL, "url", "", "LLM provider endpoint URL（必选）")
    proxySniffCmd.Flags().StringVar(&sniffKey, "key", "", "LLM provider API key（必选）")
    proxySniffCmd.MarkFlagRequired("url")
    proxySniffCmd.MarkFlagRequired("key")
    proxySniffCmd.Flags().BoolVarP(&sniffVerbose, "verbose", "v", false, "显示每个模型的详细信息")

    proxyDbAddCmd.Flags().StringVar(&sniffURL, "url", "", "LLM provider endpoint URL（必选）")
    proxyDbAddCmd.Flags().StringVar(&sniffKey, "key", "", "LLM provider API key（必选）")
    proxyDbAddCmd.MarkFlagRequired("url")
    proxyDbAddCmd.MarkFlagRequired("key")
    proxyDbAddCmd.Flags().BoolVarP(&sniffVerbose, "verbose", "v", false, "显示每个模型的详细信息")

    proxyCmd.AddCommand(proxyDetectCmd)
    proxyCmd.AddCommand(proxyRouteCmd)
    proxyCmd.AddCommand(proxySniffCmd)
    proxyCmd.AddCommand(proxyDbCmd)
    proxyDbCmd.AddCommand(proxyDbAddCmd)
    proxyDbCmd.AddCommand(proxyDbListCmd)
    proxyDbCmd.AddCommand(proxyDbShowCmd)
    proxyDbCmd.AddCommand(proxyDbRmCmd)
    proxyDbCmd.AddCommand(proxyDbQueryCmd)
}

// ========== CONF GROUP ==========

var confCmd = &cobra.Command{
    Use:   "conf",
    Short: "配置管理（备份、快照、回滚、分支）",
    Long: `配置管理命令组，用于备份、快照、回滚 agent 配置文件。

子命令：
  bak       备份所有配置（创建快照）
  history   列出所有配置快照
  show      创建配置快照
  rollback  恢复到指定快照
  diff      对比两个快照的差异
  branch    管理配置分支
`,
}

var backupBranch string
var backupMessage string

var confBakCmd = &cobra.Command{
    Use:   "bak",
    Short: "备份所有 agent 配置文件（带版本信息）",
    Long: `备份所有已安装 agent 的配置文件，自动生成版本化快照。

快照元数据存储在 ~/.codex/backups/versioning.json
原始备份文件存储在 ~/.codex/backups/snapshots/<时间戳>/

示例:
  agent-nexus conf bak                                          # 默认分支 main
  agent-nexus conf bak --branch production                      # 指定分支
  agent-nexus conf bak --message "配置更新前快照"                 # 添加提交信息
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

var confHistoryCmd = &cobra.Command{
    Use:   "history",
    Short: "列出所有配置快照（版本历史）",
    Long: `显示所有历史配置快照，包括时间戳、分支、提交信息和包含的文件。

示例:
  agent-nexus conf history                                          # 显示所有快照
  agent-nexus conf history --branch main                            # 只显示主分支
`,
    RunE: func(cmd *cobra.Command, args []string) error {
        home := userHomeDir()
        destRoot := filepath.Join(home, ".codex", "backups")
        r := versioning.LoadRegistry(destRoot)

        fmt.Printf("\n配置版本历史 (%d 个快照):\n", len(r.Snapshots))
        fmt.Println(strings.Repeat("-", 80))

        snapshots := r.ListSnapshots()
        if len(snapshots) == 0 {
            fmt.Println("  无快照。使用 'agent-nexus conf bak' 创建第一个快照。")
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

        if len(r.Branches) > 1 {
            fmt.Printf("\n  可用分支: %s\n", strings.Join(r.BranchesList(), ", "))
            fmt.Printf("  当前分支: %s\n", r.CurrentBranch)
        }

        fmt.Println()
        return nil
    },
}

var snapshotBranch string
var snapshotMessage string

var confShowCmd = &cobra.Command{
    Use:   "show",
    Short: "创建配置快照（快照/提交）",
    Long: `创建配置快照，类似 git commit。快照包含所有可配置 agent 的当前配置内容和元数据。

快照会自动保存到 ~/.codex/backups/snapshots/<时间戳>/
元数据存储在 ~/.codex/backups/versioning.json

示例:
  agent-nexus conf show --message "初始配置"
  agent-nexus conf show --branch dev --message "开发分支配置"
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

var rollbackID string

var confRollbackCmd = &cobra.Command{
    Use:   "rollback -s <snapshot-id>",
    Short: "恢复到指定快照",
    Long: `从指定的历史快照恢复 agent 配置文件。

使用 'agent-nexus conf history' 查看所有可用的快照 ID。

示例:
  agent-nexus conf rollback -s 2026-07-17_14-30-00    # 恢复指定快照
  agent-nexus conf rollback -s latest                  # 恢复到最新快照
`,
    RunE: func(cmd *cobra.Command, args []string) error {
        if rollbackID == "" {
            return fmt.Errorf("请指定快照 ID（使用 -s 参数，或输入 'latest' 恢复最新快照）")
        }

        home := userHomeDir()
        destRoot := filepath.Join(home, ".codex", "backups")
        r := versioning.LoadRegistry(destRoot)

        targetID := rollbackID
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

var diffOld string
var diffNew string

var confDiffCmd = &cobra.Command{
    Use:   "diff --old <id> --new <id>",
    Short: "对比两个快照的差异",
    Long: `比较两个版本快照之间的配置变更，显示新增、删除和修改的文件。

使用 'agent-nexus conf history' 查看所有可用快照 ID。
使用 'latest' 表示最新快照。

示例:
  agent-nexus conf diff --old 2026-07-17_14-30-00 --new 2026-07-17_15-00-00
  agent-nexus conf diff --old latest --new 2026-07-17_14-30-00
`,
    RunE: func(cmd *cobra.Command, args []string) error {
        if diffOld == "" || diffNew == "" {
            return fmt.Errorf("请指定 --old 和 --new 快照 ID（使用 'agent-nexus conf history' 查看可用快照）")
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

var branchCreateName string
var branchSwitchName string
var branchShow bool

var confBranchCmd = &cobra.Command{
    Use:   "branch",
    Short: "管理配置分支",
    Long: `管理配置快照的分支，类似 git branch。

用法:
  agent-nexus conf branch create <name>     创建新分支
  agent-nexus conf branch switch <name>     切换到指定分支
  agent-nexus conf branch list              列出所有分支
  agent-nexus conf branch show              显示当前分支信息

示例:
  agent-nexus conf branch create production    # 创建生产分支
  agent-nexus conf branch switch production    # 切换到生产分支
  agent-nexus conf bak --branch production     # 在指定分支上创建快照
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

func initConfCmd() {
    confBakCmd.Flags().StringVar(&backupBranch, "branch", "main", "快照所属分支名称")
    confBakCmd.Flags().StringVar(&backupMessage, "message", "", "快照提交信息")

    confShowCmd.Flags().StringVar(&snapshotBranch, "branch", "main", "快照所属分支名称")
    confShowCmd.Flags().StringVar(&snapshotMessage, "message", "", "快照提交信息")

    confRollbackCmd.Flags().StringVarP(&rollbackID, "snapshot", "s", "", "要恢复到的快照 ID（输入 'latest' 恢复最新快照）")
    confRollbackCmd.MarkFlagRequired("snapshot")

    confDiffCmd.Flags().StringVar(&diffOld, "old", "", "旧快照 ID（或 'latest'）")
    confDiffCmd.Flags().StringVar(&diffNew, "new", "", "新快照 ID（或 'latest'）")
    confDiffCmd.MarkFlagRequired("old")
    confDiffCmd.MarkFlagRequired("new")

    confBranchCmd.Flags().StringVar(&branchCreateName, "create", "", "创建新分支名称")
    confBranchCmd.Flags().StringVar(&branchSwitchName, "switch", "", "切换到指定分支")
    confBranchCmd.Flags().BoolVar(&branchShow, "show", false, "显示当前分支信息")

    confCmd.AddCommand(confBakCmd)
    confCmd.AddCommand(confHistoryCmd)
    confCmd.AddCommand(confShowCmd)
    confCmd.AddCommand(confRollbackCmd)
    confCmd.AddCommand(confDiffCmd)
    confCmd.AddCommand(confBranchCmd)
}

// ========== INIT ==========

func init() {
    initAgentCmd()
    initProxyCmd()
    initConfCmd()
}

// ========== UTILITY FUNCTIONS ==========

func installAllRuntimes() error {
    agents := install.AllRuntimes()
    fmt.Printf("\n正在安装 %d 个 CLI agent 运行时...\n", len(agents))
    fmt.Println(strings.Repeat("-", 60))
    for _, a := range agents {
        cmdStr, isNpm, isPip := a.InstallCommand()
        if cmdStr == "" || strings.HasPrefix(cmdStr, "No install") {
            continue
        }
        fmt.Printf("  正在安装 %s ...", a.Name)
        if isNpm {
            if err := executeNpmCommand(fmt.Sprintf("install -g %s", a.NpmPackage), false); err != nil {
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
    fmt.Printf("\n安装完成，运行: agent-nexus agent discover 确认安装成功\n")
    return nil
}

func executeNpmCommand(args string, force bool) error {
    argsList := strings.Fields(args)
    if force {
        argsList = append(argsList, "--force")
    }
    cmd := exec.Command("npm", argsList...)
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    return cmd.Run()
}

func executePipCommand(args string) error {
    cmd := exec.Command("pip", strings.Fields(args)...)
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    return cmd.Run()
}

func executeCommand(fullCmd string) error {
    psPath := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
    cmd := exec.Command(psPath, "-Command", fullCmd)
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    return cmd.Run()
}

func parseInt(s string) int {
    var result int
    for _, c := range s {
        if c >= '0' && c <= '9' {
            result = result*10 + int(c-'0')
        }
    }
    return result
}

// Execute runs the root command
func Execute() {
    if err := rootCmd.Execute(); err != nil {
        os.Exit(1)
    }
}
