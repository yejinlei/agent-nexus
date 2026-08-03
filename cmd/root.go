package cmd

import (
	"agent-nexus/internal/db"
	"agent-nexus/internal/discover"
	"agent-nexus/internal/install"
	"agent-nexus/internal/model"
	"agent-nexus/internal/pre"
	"agent-nexus/internal/proxy"
	"agent-nexus/internal/sniff"
	"agent-nexus/internal/versioning"
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
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
        codebuddy, hermes, qoder, trae, pi
  IDE:  cursor (via openai-compatible provider)
  不可配置: antigravity, copilot, Deveco, qoder-ide, trae-ide,
            codebuddy-ide, windsurf, zed

用法：
  agent-nexus agent discover [-v]   扫描已安装的 agent（-v 显示支持模型）
  agent-nexus agent list            显示可安装的 agent 列表
  agent-nexus agent install <name>  安装 agent 运行时
  agent-nexus agent uninstall <name> 卸载指定 agent
  agent-nexus agent update <name>   更新指定 agent
  agent-nexus proxy detect          检测 AI 代理配置并嗅探上游模型（统一入口）
  agent-nexus proxy route           显示模型路由表
  agent-nexus proxy check           检查已保存代理是否仍然有效
  agent-nexus db add                嗅探代理并保存到数据库
  agent-nexus db list               列出已保存的代理配置
  agent-nexus db show <id>          显示指定代理配置详情
  agent-nexus db rm <id>            删除指定代理配置
  agent-nexus db query [filter]     查询代理配置
  agent-nexus db check <id>         嗅探代理配置是否仍然有效
  agent-nexus conf backup               手动备份（替代 conf bak/show）
  agent-nexus conf history          列出所有配置快照
  agent-nexus conf rollback -s <id> 恢复到指定快照
  agent-nexus conf diff --old --new 对比两个快照的差异
  agent-nexus conf set --agent all  统一配置入口
  agent-nexus conf branch           管理配置分支
  agent-nexus pre check              检查 agent 运行时依赖工具状态
  agent-nexus pre install            安装缺失的 agent 运行时依赖工具

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

	rootCmd.AddCommand(dbCmd)
	rootCmd.AddCommand(agentCmd)
	rootCmd.AddCommand(proxyCmd)
	rootCmd.AddCommand(confCmd)
	rootCmd.AddCommand(preCmd)
	initDbCmd()
	initPreCmd()
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

// ========== PRE COMMAND ==========

// preCmd manages pre-install dependency checks for agent runtimes.
var preCmd = &cobra.Command{
	Use:   "pre",
	Short: "检查/安装 agent 运行时依赖工具",
	Long:  "检查并安装运行 AI agent 所需的依赖工具（node/npm、python/pip、git）。",
}

var preTool string = "all"

var preCheckCmd = &cobra.Command{
	Use:   "check [--tool node|pip|git|all]",
	Short: "检查依赖工具状态",
	Long:  "打印依赖工具的状态表。使用 --tool 筛选单个工具。",
	RunE: func(cmd *cobra.Command, args []string) error {
		filtered := pre.GetByTool(preTool)
		if filtered == nil {
			return fmt.Errorf(`无效的 --tool 值 %s，可选: node, pip, git, all`, preTool)
		}
		statuses := make([]pre.Status, len(filtered))
		for i, d := range filtered {
			statuses[i] = d.Check()
		}
		pre.RenderStatusTable(filtered, statuses)
		return nil
	},
}

var preInstallCmd = &cobra.Command{
	Use:   "install [--tool node|pip|git|all]",
	Short: "安装缺失的依赖工具",
	Long:  "尝试通过 winget/apt/yum/brew 自动安装缺失的依赖工具。失败时打印手动安装提示。",
	RunE: func(cmd *cobra.Command, args []string) error {
		filtered := pre.GetByTool(preTool)
		if filtered == nil {
			return fmt.Errorf(`无效的 --tool 值 %s，可选: node, pip, git, all`, preTool)
		}
		fmt.Printf("正在安装依赖工具（--tool=%s）...\n", preTool)
		fmt.Println(pre.Separator(60))
		ok, fail := pre.InstallMissing(preTool)
		if ok > 0 {
			fmt.Printf("\n%s %d 个工具已就绪\n", pre.SuccessIcon(), ok)
		}
		if fail > 0 {
			fmt.Printf("%s %d 个工具安装失败，请手动安装\n", pre.FailIcon(), fail)
		}
		return nil
	},
}

func initPreCmd() {
	preCheckCmd.Flags().StringVar(&preTool, "tool", "all", "筛选工具: node, pip, git, all")
	preInstallCmd.Flags().StringVar(&preTool, "tool", "all", "筛选工具: node, pip, git, all")
	preCmd.AddCommand(preCheckCmd)
	preCmd.AddCommand(preInstallCmd)
}

// ========== AGENT GROUP ==========

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Agent 管理（发现、安装、卸载、更新、配置、模型）",
	Long: `Agent 管理命令组，用于发现本机已安装的 AI agent、安装/卸载/更新 agent 运行时。

子命令：
  discover  扫描已安装的 agent
  list      显示可安装的 agent 列表
  install   安装指定 agent
  uninstall 卸载指定 agent
  update    更新指定 agent
  models    显示 agent 支持的模型及模型支持情况
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
			if err != nil || p == nil {
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
			cmdStr, _, _, _ := a.InstallCommand()
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
		// Check if already installed (skip unless --force)
		if !installForce {
			discovered := discover.Discover()
			for _, d := range discovered {
				if d.Name == name && d.HasConfig {
					fmt.Printf("✅ %s 已安装，跳过\n\n安装后确认: agent-nexus agent discover 查看配置状态\n", d.Name)
					return nil
				}
			}
		}
		platform := install.CurrentPlatform()
		fmt.Printf("正在安装 %s (%s) 到 %s...\n", a.Display, platform, a.Notes)
		fmt.Println()
		cmdStr, isNpm, isPip, isScript := a.InstallCommand()
		if installExecute {
			fmt.Println("正在执行...")
			if isNpm {
				if err := executeNpmCommand(fmt.Sprintf("install -g %s", a.NpmPackage), installForce); err != nil {
					return fmt.Errorf("安装失败: %v", err)
				}
				if err := verifyInstalled(name); err != nil {
					return fmt.Errorf("安装完成但后验证失败: %v", err)
				}
				fmt.Println("✅ 安装完成")
				// On Windows, npm packages register .ps1 proxy scripts that require
				// PowerShell execution policy to allow running scripts.
				if runtime.GOOS == "windows" {
					if err := enablePowerShellScripts(); err != nil {
						fmt.Printf("\n⚠ 设置 PowerShell 执行策略失败: %v\n", err)
						fmt.Printf("  请手动运行: Set-ExecutionPolicy RemoteSigned -Scope CurrentUser\n")
					}
				}
			} else if isPip {
				if err := executePipCommand(fmt.Sprintf("install %s", a.PipPackage)); err != nil {
					return fmt.Errorf("安装失败: %v", err)
				}
				if err := verifyInstalled(name); err != nil {
					return fmt.Errorf("安装完成但后验证失败: %v", err)
				}
				fmt.Println("✅ 安装完成")
			} else if cmdStr == "" || strings.HasPrefix(cmdStr, "No install") {
				fmt.Printf("当前平台 (%s) 无可用安装命令\n", platform)
				fmt.Printf("\n所有可用下载地址:\n")
				for p, url := range a.Download {
					fmt.Printf("  %s: %s\n", p, url)
				}
				if a.NpmPackage != "" {
					fmt.Printf("\n如果通过 npm 安装: %s\n", "npm install -g "+a.NpmPackage)
				}
				if a.PipPackage != "" {
					fmt.Printf("\n如果通过 pip 安装: %s\n", "pip install "+a.PipPackage)
				}
			} else if isScript {
				fmt.Println("正在下载并执行安装脚本...")
				if err := executeRemoteScript(cmdStr); err != nil {
					return fmt.Errorf("安装失败: %v", err)
				}
				if err := verifyInstalled(name); err != nil {
					return fmt.Errorf("安装脚本返回成功但后验证失败: %v", err)
				}
				fmt.Println("✅ 安装完成")
			} else {
				if strings.HasPrefix(cmdStr, "http://") || strings.HasPrefix(cmdStr, "https://") {
					fmt.Println("正在下载并执行安装文件...")
					if err := executeDownloadedFile(cmdStr); err != nil {
						return fmt.Errorf("安装失败: %v", err)
					}
				} else {
					if err := executeCommand(cmdStr); err != nil {
						return fmt.Errorf("安装失败: %v", err)
					}
				}
				if err := verifyInstalled(name); err != nil {
					return fmt.Errorf("安装命令返回成功但后验证失败: %v", err)
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
					filepath.Join(home, "AppData", "Local", name, "bin", name),
					filepath.Join(home, "AppData", "Local", name, name+".exe"),
					filepath.Join(home, "AppData", "Local", name, name+"-agent", "venv", "Scripts", name+".exe"),
					filepath.Join(home, "AppData", "Local", name+"-code", "bin", name+".exe"),
					filepath.Join(home, "AppData", "Local", name+"-code", "bin", name),
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
					fmt.Printf("\n⚠ 未找到已安装的二进制文件，请检查安装日志或手动查找\n")
					fmt.Printf("常见位置: ~/.%s-code/bin/、~/.%s/bin/ 或 AppData\\Local\\%s\\*\n", name, name, name)
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
					fmt.Printf("\n如果通过 npm 安装: %s\n", "npm install -g "+a.NpmPackage)
				}
				if a.PipPackage != "" {
					fmt.Printf("\n如果通过 pip 安装: %s\n", "pip install "+a.PipPackage)
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
		updateCmd, isNpm, isPip, isScript := ua.UpdateCommand()
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
		} else if isScript {
			fmt.Printf("更新命令: 下载并执行安装脚本\n")
			if installExecute {
				fmt.Println("正在下载并执行安装脚本...")
				if err := executeRemoteScript(updateCmd); err != nil {
					return fmt.Errorf("更新失败: %v", err)
				}
				fmt.Println("✅ 更新完成")
			} else {
				fmt.Printf("\n安装脚本地址: %s\n", updateCmd)
				fmt.Printf("\n运行以下命令完成更新:\n  下载并执行上述安装脚本\n")
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
	initModelsCmds()
	agentCmd.AddCommand(agentModelsCmd)
	proxyCmd.AddCommand(proxyModelsCmd)
	confCmd.AddCommand(confModelsCmd)
}

// ========== PROXY GROUP ==========

var proxyCmd = &cobra.Command{
	Use:   "proxy",
	Short: "AI 消息网关管理（检测、路由、嗅探、检查）",
	Long: `代理管理命令组，用于检测 AI 代理配置、嗅探上游模型、显示模型路由、检查代理有效性。

子命令：
  detect    检测 AI 代理配置 + 嗅探上游模型（统一入口）
  route     显示模型路由表
  check     检查已保存代理是否仍然有效
`,
}

var proxyDetectCmd = &cobra.Command{
	Use:   "detect",
	Short: "检测 AI 代理配置并嗅探上游模型",
	Long: `检测 AI 代理配置（本机 CCX Desktop / CC-Switch 或自定义），嗅探上游模型列表。

用法：
  agent-nexus proxy detect                            自动检测本机代理 + 嗅探模型
  agent-nexus proxy detect --url <url> --key <key>   探测指定 AI 网关
  agent-nexus proxy detect --db <N|all>               从数据库读取已保存的网关模型
  agent-nexus proxy detect --no-sniff                仅显示配置信息，不嗅探
  agent-nexus proxy detect -v                         详细模式，显示完整模型列表
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Mode 1: --db flag → 从数据库读取
		if proxyDetectDB != "" {
			return runProxyModels(proxyDetectDB, proxyDetectVerbose)
		}

		// Mode 2: auto-detect 或 --url/--key
		p, err := getProxySettings()
		if err != nil || p == nil {
			fmt.Printf("未能检测到 AI 代理配置: %v\n", err)
			return err
		}

		// 显示代理配置
		fmt.Printf("\nAI 代理配置:\n")
		fmt.Printf("  地址:   %s\n", p.BaseURL)
		fmt.Printf("  密钥:   %s\n", p.APIKey)
		if len(p.ModelMap) > 0 {
			fmt.Printf("\n  模型映射表 (%d 条):\n", len(p.ModelMap))
			for src, dst := range p.ModelMap {
				fmt.Printf("    %-15s → %s\n", src, dst)
			}
		}

		// Mode 3: 嗅探并显示模型
		if !proxyDetectNoSniff {
			fmt.Println()
			return runSniffAndSave(p.BaseURL, p.APIKey, proxyDetectVerbose)
		}

		fmt.Println()
		return nil
	},
}

// runSniffAndSave 探测 AI 网关 endpoint，打印结果并自动保存到数据库
func runSniffAndSave(baseURL, apiKey string, verbose bool) error {
	result, err := sniff.Sniff(baseURL, apiKey)
	if err != nil {
		fmt.Printf("嗅探失败: %v\n", err)
		return err
	}

	fmt.Printf("嗅探结果: %s\n", result.BaseURL)
	fmt.Printf("  检测格式: %s\n", result.DetectedFormat)
	fmt.Printf("  OpenAI 兼容: %v\n", result.OpenAICap)
	fmt.Printf("  Anthropic 兼容: %v\n", result.AnthropicCap)
	fmt.Printf("  模型数量: %d\n", result.ModelCount)
	if result.Notes != "" {
		fmt.Printf("  备注: %s\n", result.Notes)
	}
	if verbose {
		fmt.Printf("\n  模型列表 (%d):\n", len(result.Models))
		for i, m := range result.Models {
			fmt.Printf("  %3d. %s\n", i+1, m.ID)
		}
	}
	fmt.Println()

	// 自动保存到数据库
	if result.ModelCount > 0 {
		dbPath := filepath.Join(userHomeDir(), ".agent-nexus", "proxies.db")
		dir := filepath.Dir(dbPath)
		if mkdirErr := os.MkdirAll(dir, 0o755); mkdirErr != nil {
			fmt.Printf("⚠ 保存失败 (创建目录): %v\n", mkdirErr)
		} else {
			dbInst, openErr := db.New()
			if openErr != nil {
				fmt.Printf("⚠ 保存失败 (打开数据库): %v\n", openErr)
			} else {
				defer dbInst.Close()
				if initErr := dbInst.Init(); initErr != nil {
					fmt.Printf("⚠ 保存失败 (初始化数据库): %v\n", initErr)
				} else if dbInst.ExistsByURL(result.BaseURL) {
					fmt.Printf("ℹ 已存在，跳过重复添加: %s\n", result.BaseURL)
				} else {
					modelIDs := make([]string, 0, len(result.Models))
					for _, m := range result.Models {
						modelIDs = append(modelIDs, m.ID)
					}
					if addErr := dbInst.Add(result.BaseURL, apiKey, result.DetectedFormat, result.OpenAICap, result.AnthropicCap, result.ModelCount, modelIDs, time.Now()); addErr != nil {
						fmt.Printf("⚠ 保存失败: %v\n", addErr)
					} else {
						fmt.Printf("✅ 已自动保存到数据库: %s\n", result.BaseURL)
					}
				}
			}
		}
	}

	return nil
}

var proxyCheckCmd = &cobra.Command{
	Use:   "check <id>",
	Short: "检查已保存代理是否仍然有效",
	Long: `检查数据库中已保存的代理配置是否仍然有效。

用法：
  agent-nexus proxy check <id>    检查指定 ID 的记录
  agent-nexus proxy check --all   检查所有记录

对无效记录会交互提示是否删除。
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runProxyCheck(args, checkAll)
	},
}

// runProxyCheck 检查代理有效性的共享逻辑，供 proxy check 和 db check（兼容别名）使用。
func runProxyCheck(args []string, all bool) error {
	dbInst, err := db.New()
	if err != nil {
		return fmt.Errorf("打开数据库失败: %v", err)
	}
	defer dbInst.Close()
	if err := dbInst.Init(); err != nil {
		return fmt.Errorf("初始化数据库失败: %v", err)
	}

	var records []db.ProxyRecord
	if all {
		records, err = dbInst.List()
		if err != nil {
			return fmt.Errorf("读取数据库失败: %v", err)
		}
	} else {
		if len(args) < 1 {
			return fmt.Errorf("请指定代理配置 ID 或使用 --all\n\n用法: agent-nexus proxy check <id>\n    agent-nexus proxy check --all")
		}
		id := parseInt(args[0])
		record, getErr := dbInst.GetByID(id)
		if getErr != nil {
			return fmt.Errorf("查询 ID %s 失败: %v", args[0], getErr)
		}
		if record == nil {
			fmt.Printf("未找到 ID 为 %s 的代理配置\n", args[0])
			return nil
		}
		records = []db.ProxyRecord{*record}
	}

	if len(records) == 0 {
		fmt.Println("数据库为空，没有可检查的记录。")
		return nil
	}

	reader := bufio.NewReader(os.Stdin)
	deleted := 0

	for _, rec := range records {
		fmt.Printf("\n[检查] ID=%d %s ...", rec.ID, rec.URL)
		result, sniffErr := sniff.Sniff(rec.URL, rec.Key)

		if sniffErr != nil {
			fmt.Printf("\n  ❌ 无效: %v\n", sniffErr)
			fmt.Printf("  该代理已失效，是否删除？(yes/no): ")
			answer, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("读取输入失败: %v", err)
			}
			answer = strings.TrimSpace(strings.ToLower(answer))
			if answer == "yes" {
				if delErr := dbInst.Delete(rec.ID); delErr != nil {
					fmt.Printf("  删除失败: %v\n", delErr)
				} else {
					fmt.Printf("  ✅ 已删除 ID=%d\n", rec.ID)
					deleted++
				}
			} else {
				fmt.Printf("  保留 ID=%d\n", rec.ID)
			}
		} else if result.DetectedFormat == "" && result.ModelCount == 0 {
			fmt.Printf("\n  ❌ 无效: HTTP 200 但未检测到任何格式或模型\n")
			fmt.Printf("  该代理已失效，是否删除？(yes/no): ")
			answer, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("读取输入失败: %v", err)
			}
			answer = strings.TrimSpace(strings.ToLower(answer))
			if answer == "yes" {
				if delErr := dbInst.Delete(rec.ID); delErr != nil {
					fmt.Printf("  删除失败: %v\n", delErr)
				} else {
					fmt.Printf("  ✅ 已删除 ID=%d\n", rec.ID)
					deleted++
				}
			} else {
				fmt.Printf("  保留 ID=%d\n", rec.ID)
			}
		} else {
			fmt.Printf(" ✅ 有效 (格式: %s, 模型: %d)\n", result.DetectedFormat, result.ModelCount)
		}
	}

	fmt.Printf("\n检查完成，共 %d 条记录，删除 %d 条\n", len(records), deleted)
	return nil
}

var proxyRouteCmd = &cobra.Command{
	Use:   "route",
	Short: "显示模型路由表",
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := getProxySettings()
		if err != nil || p == nil {
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
var proxyDetectDB string
var proxyDetectNoSniff bool
var proxyDetectVerbose bool
var rmAll bool
var checkAll bool

var proxySniffCmd = &cobra.Command{
	Use:   "sniff",
	Short: "[已弃用] 请使用 proxy detect --url <url> --key <key>",
	Long: `嗅探 LLM 提供商的 endpoint，自动检测其支持的消息格式和可用模型列表。

[已弃用] 推荐使用统一入口:
  agent-nexus proxy detect --url <url> --key <key>
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// 委托给 proxy detect
		proxyURL = sniffURL
		proxyKey = sniffKey
		proxyDetectVerbose = sniffVerbose
		proxyDetectNoSniff = false
		return proxyDetectCmd.RunE(cmd, args)
	},
}

func initProxyCmd() {
	proxySniffCmd.Flags().StringVar(&sniffURL, "url", "", "LLM provider endpoint URL（必选）")
	proxySniffCmd.Flags().StringVar(&sniffKey, "key", "", "LLM provider API key（必选）")
	proxySniffCmd.MarkFlagRequired("url")
	proxySniffCmd.MarkFlagRequired("key")
	proxySniffCmd.Flags().BoolVarP(&sniffVerbose, "verbose", "v", false, "显示每个模型的详细信息")

	// proxy detect flags
	proxyDetectCmd.Flags().StringVar(&proxyDetectDB, "db", "", "从数据库读取：<N>=指定id，all=全部")
	proxyDetectCmd.Flags().BoolVar(&proxyDetectNoSniff, "no-sniff", false, "仅显示配置，不嗅探")
	proxyDetectCmd.Flags().BoolVarP(&proxyDetectVerbose, "verbose", "v", false, "显示完整模型列表")

	proxyCmd.AddCommand(proxyDetectCmd)
	proxyCmd.AddCommand(proxyRouteCmd)
	proxyCmd.AddCommand(proxyCheckCmd)
	proxyCmd.AddCommand(proxySniffCmd)

	proxyCmd.AddCommand(&cobra.Command{
		Use:           "db",
		Hidden:        true,
		SilenceUsage:  true,
		SilenceErrors: false,
		RunE: func(*cobra.Command, []string) error {
			return fmt.Errorf("命令已移除。请使用 'agent-nexus db'（顶层命令）或 'agent-nexus proxy check'：\n\n  agent-nexus db list\n  agent-nexus db add -u <url> -k <key>\n  agent-nexus db show <id>\n  agent-nexus db rm <id>\n  agent-nexus db query [filter]\n  agent-nexus proxy check <id>")
		},
	})
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
	Short: "备份所有 agent 配置文件（已弃用，请使用 conf backup）",
	Long: `备份所有已安装 agent 的配置文件，自动生成版本化快照。

快照元数据存储在 ~/.codex/backups/versioning.json
原始备份文件存储在 ~/.codex/backups/snapshots/<时间戳>/

示例:
  agent-nexus conf bak                                          # 默认分支 main
  agent-nexus conf bak --branch production                      # 指定分支
  agent-nexus conf bak --message "配置更新前快照"                 # 添加提交信息
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("[WARNING] 'conf bak' 已弃用，请使用 'conf backup' 代替\n")
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
			fmt.Printf("  ✅ %s  [%s, %d bytes]\n",
				filepath.Base(p), entry.SHA256[:8], entry.Bytes)
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
	Short: "创建配置快照（已弃用，请使用 conf backup --message）",
	Long: `创建配置快照，类似 git commit。快照包含所有可配置 agent 的当前配置内容和元数据。

快照会自动保存到 ~/.codex/backups/snapshots/<时间戳>/
元数据存储在 ~/.codex/backups/versioning.json

示例:
  agent-nexus conf show --message "初始配置"
  agent-nexus conf show --branch dev --message "开发分支配置"
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("[WARNING] 'conf show' 已弃用，请使用 'conf backup --message <msg>' 代替\n")
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
			fmt.Printf("  ✅ %s [%s, %d bytes]\n", filepath.Base(p), entry.SHA256[:8], entry.Bytes)
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

	initConfSetCmd()
	initConfBackupCmd()
	initConfAutoCmd()
	confBakCmd.Flags().StringVar(&backupBranch, "branch", "main", "快照所属分支名称（已弃用）")
	confBakCmd.Flags().StringVar(&backupMessage, "message", "", "快照提交信息（已弃用）")

	confShowCmd.Flags().StringVar(&snapshotBranch, "branch", "main", "[已弃用] 快照所属分支名称，请使用 conf backup --branch")
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
	confCmd.AddCommand(confBackupCmd)
	confCmd.AddCommand(confHistoryCmd)
	confCmd.AddCommand(confShowCmd)
	confCmd.AddCommand(confRollbackCmd)
	confCmd.AddCommand(confDiffCmd)
	confCmd.AddCommand(confBranchCmd)
	confCmd.AddCommand(confSetCmd)
	confCmd.AddCommand(confAutoCmd)

	initConfUpstreamModels()
}

// ========== INIT ==========

func init() {
	initAgentCmd()
	initProxyCmd()
	initConfCmd()
}

// ========== UTILITY FUNCTIONS ==========

func verifyInstalled(name string) error {
	discovered := discover.Discover()
	for _, d := range discovered {
		if d.Name == name && d.HasConfig {
			return nil
		}
	}
	return fmt.Errorf("%s 未检测到安装", name)
}

func installAllRuntimes() error {
	// Collect set of already-installed agent names from discovery
	discovered := discover.Discover()
	installedSet := make(map[string]bool)
	for _, a := range discovered {
		if a.HasConfig {
			installedSet[a.Name] = true
		}
	}

	agents := install.AllRuntimes()
	skipped := 0
	installed := 0
	for _, a := range agents {
		if _, ok := installedSet[a.Name]; ok {
			fmt.Printf("  %s ...", a.Name)
			fmt.Println(" ⏭ 已安装，跳过")
			skipped++
			continue
		}
		cmdStr, isNpm, isPip, isScript := a.InstallCommand()
		if cmdStr == "" || strings.HasPrefix(cmdStr, "No install") {
			fmt.Printf("  %s ...", a.Name)
			fmt.Println(" ⏭ 当前平台无可用安装命令")
			skipped++
			continue
		}
		fmt.Printf("  正在安装 %s ...", a.Name)
		if isNpm {
			if err := executeNpmCommand(fmt.Sprintf("install -g %s", a.NpmPackage), false); err != nil {
				fmt.Printf(" ❌ %v\n", err)
			} else {
				fmt.Println(" ✅")
				installed++
			}
		} else if isPip {
			if err := executePipCommand(fmt.Sprintf("install %s", a.PipPackage)); err != nil {
				fmt.Printf(" ❌ %v\n", err)
			} else {
				fmt.Println(" ✅")
				installed++
			}
		} else if isScript {
			if err := executeRemoteScript(cmdStr); err != nil {
				fmt.Printf(" ❌ %v\n", err)
			} else {
				fmt.Println(" ✅")
				installed++
			}
		} else {
			if strings.HasPrefix(cmdStr, "http://") || strings.HasPrefix(cmdStr, "https://") {
				if err := executeDownloadedFile(cmdStr); err != nil {
					fmt.Printf(" ❌ %v\n", err)
				} else {
					fmt.Println(" ✅")
					installed++
				}
			} else {
				if err := executeCommand(cmdStr); err != nil {
					fmt.Printf(" ❌ %v\n", err)
				} else {
					fmt.Println(" ✅")
					installed++
				}
			}
		}
	}
	fmt.Printf("\n安装完成: %d 个新安装, %d 个跳过。运行: agent-nexus agent discover 确认安装成功\n", installed, skipped)
	// On Windows, enable PowerShell execution policy so npm-installed .ps1 scripts can run.
	if runtime.GOOS == "windows" {
		if err := enablePowerShellScripts(); err != nil {
			fmt.Printf("\n⚠ 设置 PowerShell 执行策略失败: %v\n", err)
			fmt.Printf("  请手动运行: Set-ExecutionPolicy RemoteSigned -Scope CurrentUser\n")
		}
	}
	return nil
}

// enablePowerShellScripts sets CurrentUser execution policy to RemoteSigned,
// allowing npm-installed .ps1 proxy scripts (e.g. codex.ps1, claude.ps1) to run.
func enablePowerShellScripts() error {
	if runtime.GOOS != "windows" {
		return nil
	}
	psPath := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	cmd := exec.Command(psPath, "-NoProfile", "-Command", "Set-ExecutionPolicy", "RemoteSigned", "-Scope", "CurrentUser")
	cmd.Stdout = nil
	cmd.Stderr = nil
	return cmd.Run()
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

// executeRemoteScript downloads a remote installer script (install.ps1/install.sh)
// using Go's HTTP client, saves it to a temp .ps1 file, and executes it with
// powershell.exe -File. Using -File instead of -Command is critical on
// Windows PowerShell 5.1: -Command with -NoProfile prevents the auto-loading
// of core modules (Microsoft.PowerShell.Utility, Microsoft.PowerShell.Security
// etc.) that many installers depend on (e.g. Get-FileHash for checksum
// verification). -File loads all built-in modules automatically.
//
// Go's HTTP client is used for the initial download to avoid the SSL compatibility
// issues that affect PowerShell's Invoke-RestMethod / Invoke-WebRequest on
// certain endpoints. Proxy-related environment variables are stripped from the
// subprocess environment so that Invoke-WebRequest inside the script always
// goes direct, avoiding proxy-intercepted responses that may be HTML.
func executeRemoteScript(url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download script %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download script %s: HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read script body: %w", err)
	}
	if len(body) == 0 {
		return fmt.Errorf("empty script downloaded from %s", url)
	}
	firstLine := strings.ToLower(string(body[:min(512, len(body))]))
	if strings.Contains(firstLine, "<html") || strings.Contains(firstLine, "<!doctype") || strings.Contains(firstLine, "<head") {
		return fmt.Errorf("script %s returned HTML instead of a script: %s", url, resp.Header.Get("Content-Type"))
	}
	tmpDir := os.TempDir()
	tmpFile := filepath.Join(tmpDir, "installer_"+filepath.Base(url)+".ps1")
	if err := os.WriteFile(tmpFile, body, 0644); err != nil {
		return fmt.Errorf("failed to write temp script file: %w", err)
	}
	defer os.Remove(tmpFile)
	if runtime.GOOS == "windows" {
		psPath := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
		envVars := make([]string, 0, len(os.Environ()))
		for _, ev := range os.Environ() {
			key := strings.ToUpper(strings.SplitN(ev, "=", 2)[0])
			if key == "HTTP_PROXY" || key == "HTTPS_PROXY" || key == "ALL_PROXY" || key == "NO_PROXY" {
				continue
			}
			envVars = append(envVars, ev)
		}
		cmd := exec.Command(psPath, "-ExecutionPolicy", "Bypass", "-File", tmpFile)
		cmd.Env = envVars
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	// Unix: execute via sh for shell scripts
	if strings.HasSuffix(url, ".sh") || strings.Contains(string(body), "#!/bin/sh") || strings.Contains(string(body), "#!/bin/bash") {
		cmd := exec.Command("sh", tmpFile)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	return fmt.Errorf("unsupported script type for %s on %s", url, runtime.GOOS)
}

func executeCommand(fullCmd string) error {
	psPath := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	cmd := exec.Command(psPath, "-Command", fullCmd)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// openInBrowser opens a URL in the system's default web browser.
// Returns nil on success, or the underlying error.
func openInBrowser(url string) error {
	runtime := runtime.GOOS
	switch runtime {
	case "windows":
		return exec.Command("cmd", "/c", "start", "", url).Run()
	case "darwin":
		return exec.Command("open", url).Run()
	default:
		return exec.Command("xdg-open", url).Run()
	}
}

// executeDownloadedFile downloads a file from a URL to a temp location,
// executes it, and removes it afterwards. Handles both .exe binaries and .ps1 scripts.
// If the response is HTML (a web page rather than a direct download), it opens
// the URL in the default browser so the user can complete the install manually.
func executeDownloadedFile(url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed to download %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download %s: HTTP %d", url, resp.StatusCode)
	}

	// Limit download to 100 MB to avoid downloading arbitrary large content
	maxBody := int64(100 * 1024 * 1024)
	var body []byte
	if resp.ContentLength > 0 && resp.ContentLength > maxBody {
		return fmt.Errorf("failed to download %s: content too large (%d bytes)", url, resp.ContentLength)
	}
	body, err = io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return fmt.Errorf("failed to read download body: %w", err)
	}
	if int64(len(body)) > maxBody {
		return fmt.Errorf("failed to download %s: content too large (> 100 MB)", url)
	}

	// Detect HTML response (web page rather than a direct download)
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	firstLine := strings.ToLower(string(body[:min(512, len(body))]))
	isHTML := strings.Contains(contentType, "text/html") ||
		strings.Contains(firstLine, "<html") ||
		strings.Contains(firstLine, "<!doctype") ||
		strings.Contains(firstLine, "<head") ||
		strings.Contains(firstLine, "<body")
	if isHTML {
		fmt.Printf("\n[提示] %s 是一个网页，正在打开浏览器...\n", url)
		fmt.Printf("\n请在浏览器中完成下载，安装完成后运行: agent-nexus agent discover 确认\n\n")
		_ = openInBrowser(url)
		return nil
	}

	tmpDir := os.TempDir()
	tmpName := filepath.Base(url)
	if tmpName == "" || tmpName == "." || tmpName == "/" {
		tmpName = "installer.exe"
	}
	tmpFile := filepath.Join(tmpDir, tmpName)

	// On Windows, ensure the temp file has a valid extension for the execution mode
	ext := strings.ToLower(filepath.Ext(tmpFile))
	if runtime.GOOS == "windows" && ext != ".exe" && ext != ".ps1" && ext != "" {
		// Add .exe extension if it looks like a binary
		_ = os.Remove(tmpFile)
		tmpFile = tmpFile + ".exe"
	}

	if err := os.WriteFile(tmpFile, body, 0755); err != nil {
		return fmt.Errorf("failed to write downloaded file: %w", err)
	}
	defer os.Remove(tmpFile)

	if runtime.GOOS == "windows" {
		psPath := filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
		cmd := exec.Command(psPath, "-ExecutionPolicy", "Bypass", "-NoProfile", "-File", tmpFile)
		cmd.Env = os.Environ()
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	cmd := exec.Command(tmpFile)
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// maskKey returns a desensitized API key: shows first 8 chars and last 4,
// with "..." in between. If the key is shorter than 12 chars,
// shows first 2 and last 2 with masked middle.
func maskKey(key string) string {
	if len(key) == 0 {
		return "(none)"
	}
	if len(key) <= 12 {
		// Short key: show first 2 + masked + last 2
		masked := "*"
		for i := 0; i < len(key)-4; i++ {
			masked += "*"
		}
		return key[:2] + masked + key[len(key)-2:]
	}
	// Show first 8 and last 4, mask the rest.
	return key[:8] + "..." + key[len(key)-4:]
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
