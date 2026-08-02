package cmd

import (
	"fmt"
	"os"
	"github.com/spf13/cobra"
	"path/filepath"
	"sort"
	"strings"

	"agent-nexus/internal/agent"
	"agent-nexus/internal/discover"
	"agent-nexus/internal/model"
	"agent-nexus/internal/proxy"
	"agent-nexus/internal/shared"
	"agent-nexus/internal/sniff"
	"agent-nexus/internal/versioning"
)

// runConfSetOpts carries the unified conf set options.
type runConfSetOpts struct {
	agents   string
	models   string
	branch   string
	message  string
	dryRun   bool
	dbID     string
	useDB    bool
}

var (
	confSetAgents   string
	confSetModels   string
	confSetBranch   string
	confSetMessage  string
	confSetDryRun   bool
	confSetDBID     string
	confSetUseDB    bool
)

// confSetCmd is the unified configuration entry-point.
var confSetCmd = &cobra.Command{
	Use:   "set --agents <list|all>",
	Short: "统一配置入口",
	Long: `统一配置入口，支持指定代理来源和模型覆盖。

用法：
  agent-nexus conf set --agents all
  agent-nexus conf set --agents codex --models "codex=gpt-5.5"
  agent-nexus conf set --agents all --dry-run

代理来源优先级:
  1. --url / --key 命令行标志（--key 与 --url 同时提供时生效）
  2. --db-id <id> 从数据库读取代理配置
  3. --db 显示可用代理列表并交互选择（后续实现）
  4. proxy.Detect() 自动检测

代理检测失败时返回明确的错误，不再使用伪造的 API key。
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := runConfSetOpts{
			agents:  confSetAgents,
			models:  confSetModels,
			branch:  confSetBranch,
			message: confSetMessage,
			dryRun:  confSetDryRun,
			dbID:    confSetDBID,
			useDB:   confSetUseDB,
		}
		return runConfSet(opts)
	},
}

func initConfSetCmd() {
	fs := confSetCmd.Flags()
	fs.StringVarP(&confSetAgents, "agents", "a", "all", "要配置的 agent（逗号分隔），用 all 配置所有（必选）")
	fs.StringVarP(&confSetModels, "models", "m", "", "覆盖模型映射: \"agent=模型名,agent2=模型名\"")
	fs.StringVarP(&confSetBranch, "branch", "b", "main", "快照所属分支名称")
	fs.StringVarP(&confSetMessage, "message", "c", "", "快照提交信息")
	fs.BoolVarP(&confSetDryRun, "dry-run", "d", false, "预览模式，不实际写入")
	fs.StringVar(&confSetDBID, "db-id", "", "从数据库读取指定 ID 的代理配置")
	fs.BoolVar(&confSetUseDB, "db", false, "显示可用代理列表并选择（后续实现）")
}

// runConfSet runs the unified configuration pipeline.
func runConfSet(opts runConfSetOpts) error {
	if opts.agents == "" {
		opts.agents = "all"
	}

	// Resolve proxy source
	p, src, err := getProxySource(proxyURL, proxyKey, opts.dbID, opts.useDB)
	if err != nil {
		return fmt.Errorf("获取代理配置失败: %v", err)
	}
	if p == nil {
		return fmt.Errorf("未检测到 AI 代理配置；请使用 --url 和 --key 指定代理，或确保代理正在运行")
	}
	fmt.Printf("AI 代理来源: %s (%s)\n", src, p.BaseURL)

	// Discover agents and resolve which ones to configure
	allAgents := discover.Discover()
	_, toConfigureNames, err := parseAgentsStr(opts.agents, getConfigureableAgentList())
	if err != nil {
		return err
	}
	if len(toConfigureNames) == 0 {
		fmt.Printf("未发现可配置的 agent。使用 'agent-nexus agent discover' 扫描。\n")
		return nil
	}

	// User overrides
	overrides := parseModelsStr(opts.models)

	// Build resolution table using upstream model list
	var upstreamModels []string
	if p.BaseURL != "" && p.APIKey != "" {
		fmt.Println("正在查询上游模型列表...")
		upstreamModels = sniff.UpstreamModelList(p.BaseURL, p.APIKey)
		fmt.Printf("上游可用模型 (%d)\n", len(upstreamModels))
	}

	// Resolve model for every agent in DefaultModels
	resolutions := model.ResolveAllModels(upstreamModels, p.ModelMap)
	agentToModel := make(map[string]string)
	for _, r := range resolutions {
		agentToModel[r.Agent] = r.Model
	}

	// Render resolution preview
	fmt.Println("模型分辨率览:")
	fmt.Println(strings.Repeat("-", 80))
	for _, name := range toConfigureNames {
		modelName, found := model.ModelToWrite(resolutions, overrides, name)
		if !found {
			fmt.Printf("  %-14s -> (未配置)\n", name)
			continue
		}
		notes := ""
		for _, r := range resolutions {
			if r.Agent == name {
				notes = r.Notes
				break
			}
		}
		source := ""
		for _, r := range resolutions {
			if r.Agent == name {
				source = r.Source
				break
			}
		}
		fmt.Printf("  %-14s -> %-30s [%s] %s\n", name, modelName, source, notes)
	}
	fmt.Println()

	// Dry-run: stop after preview
	if opts.dryRun {
		fmt.Printf("[预览模式 --dry-run] 未实际写入任何配置。\n")
		fmt.Println("运行不带 --dry-run 以实际配置")
		return nil
	}

	// Backup existing configs
	fmt.Println("正在备份现有配置...")
	nameToAgent := make(map[string]discover.AgentInfo)
	for _, a := range allAgents {
		nameToAgent[a.Name] = a
	}

	backupPaths := make([]string, 0, len(toConfigureNames))
	for _, name := range toConfigureNames {
		a := nameToAgent[name]
		if a.HasConfig && a.ConfigPath != "" {
			backupPaths = append(backupPaths, a.ConfigPath)
		}
	}

	var message string
	if opts.message != "" {
		message = opts.message
	} else {
		message = fmt.Sprintf("conf set: %s", opts.agents)
	}

	home := userHomeDir()
	destRoot := filepath.Join(home, ".codex", "backups")

	if len(backupPaths) > 0 {
		r := versioning.LoadRegistry(destRoot)
		s, err := r.CreateSnapshot(backupPaths, message, opts.branch)
		if err != nil {
			return fmt.Errorf("创建备份快照失败: %w", err)
		}
		fmt.Printf("  备份快照: %s\n", s.ID)
	} else {
		fmt.Println("  无现有配置文件可备份")
	}

	// Configure each agent
	fmt.Println("正在配置 agent...")
	fmt.Println(strings.Repeat("-", 60))
	registry := agent.NewWriterRegistry()
	configuredCount := 0
	failedCount := 0
	for _, name := range toConfigureNames {
		w := registry.Get(name)
		if w == nil {
			fmt.Printf("  [SKIP] %s: 不支持配置的 writer\n", name)
			failedCount++
			continue
		}

		modelName, found := model.ModelToWrite(resolutions, overrides, name)
		if !found {
			fmt.Printf("  [SKIP] %s: 无法解析模型\n", name)
			failedCount++
			continue
		}

		a := nameToAgent[name]
		var cfgPath string
		if a.ConfigPath != "" {
			cfgPath = a.ConfigPath
		} else {
			// For agents discovered via binary, generate a config path
			cfgPath = filepath.Join(home, "."+name, "config.toml")
			_ = os.MkdirAll(filepath.Dir(cfgPath), 0755)
		}

		err := w.Configure(cfgPath, p, modelName)
		if err != nil {
			fmt.Printf("  [FAIL] %s: %v\n", name, err)
			failedCount++
			continue
		}
		fmt.Printf("  [OK] %s -> %s\n", name, modelName)
		configuredCount++
	}

	fmt.Println()
	fmt.Printf("配置完成: %d 个成功, %d 个失败\n", configuredCount, failedCount)

	// Snapshot the newly written configs (as a reference)
	if configuredCount > 0 {
		fmt.Printf("\n下次运行 'agent-nexus conf set' 将检测到这些配置。\n")
	}
	return nil
}

// getConfigureableAgentList returns a comma-separated list of agent names
// that appear in the central DefaultModels map.
func getConfigureableAgentList() string {
	agents := make([]string, 0, len(shared.DefaultModels))
	for name := range shared.DefaultModels {
		agents = append(agents, name)
	}
	sort.Strings(agents)
	return strings.Join(agents, ",")
}

// parseModelsStr splits a comma-separated "agent=模型名" string into a map.
// Empty values or malformed entries are silently skipped.
func parseModelsStr(input string) map[string]string {
	m := make(map[string]string)
	for _, pair := range strings.Split(input, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			continue
		}
		agent := strings.TrimSpace(parts[0])
		model := strings.TrimSpace(parts[1])
		if agent != "" && model != "" {
			m[agent] = model
		}
	}
	return m
}

// getProxySource resolves a proxy from command-line flags, DB, or auto-detect.
// Returns the proxy, a source label, and any error.
func getProxySource(cliURL, cliKey, dbID string, useDB bool) (*proxy.Proxy, string, error) {
	// Priority 1: explicit flags
	if cliURL != "" || cliKey != "" {
		p, err := proxy.FromFlags(cliURL, cliKey)
		if err != nil {
			return nil, "flags", err
		}
		if p != nil {
			return p, "flags", nil
		}
	}

	// Priority 2: auto-detect
	p, err := proxy.Detect()
	if err == nil && p != nil {
		return p, "auto-detect", nil
	}

	// DB and interactive options: not yet implemented
	if useDB {
		fmt.Println("[TODO] 交互选择代理来源尚未实现，使用 --url/--key 指定代理。")
		return nil, "auto-detect", err
	}
	if dbID != "" {
		return nil, "db", fmt.Errorf("数据库代理查询功能尚未实现，使用 --url/--key 指定代理")
	}

	return nil, "auto-detect", err
}