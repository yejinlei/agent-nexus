package cmd

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"agent-nexus/internal/db"
	"agent-nexus/internal/discover"
	"agent-nexus/internal/install"
	"agent-nexus/internal/shared"
	"agent-nexus/internal/sniff"
	"github.com/spf13/cobra"
)

// ---- shared helpers ----

// agentNameFilter builds a list of AgentInfo for the given --agents value,
// restricted to the installable runtimes shown by "agent list". "all" returns
// every installable runtime; otherwise it is a comma-separated, case-insensitive
// list of agent names. Returns an error if any named agent is unknown.
func agentNameFilter(agentsFlag string) ([]discover.AgentInfo, error) {
	// Authoritative list = what "agent list" shows (install.AllRuntimes()).
	// Enrich each with the protocol/model fields that the models table needs
	// from the discover registry.
	runtimes := install.AllRuntimes()
	protoMap := discover.ProtocolMap()
	sourceMap := discover.ModelSourceMap()
	nativeModelsMap := discover.NativeModelsMap()
	isConfigurableMap := discover.IsConfigurableMap()

	if strings.EqualFold(agentsFlag, "all") {
		agents := make([]discover.AgentInfo, 0, len(runtimes))
		for _, r := range runtimes {
			agents = append(agents, toAgentInfo(r, protoMap, sourceMap, nativeModelsMap, isConfigurableMap))
		}
		return agents, nil
	}

	names := strings.Split(agentsFlag, ",")
	lookup := make(map[string]install.Agent)
	for _, r := range runtimes {
		lookup[strings.ToLower(r.Name)] = r
	}
	agents := make([]discover.AgentInfo, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(strings.ToLower(n))
		if n == "" {
			continue
		}
		r, ok := lookup[n]
		if !ok {
			return nil, fmt.Errorf("未知 agent: %s\n\n可用列表: agent-nexus agent list", n)
		}
		agents = append(agents, toAgentInfo(r, protoMap, sourceMap, nativeModelsMap, isConfigurableMap))
	}
	return agents, nil
}

func toAgentInfo(r install.Agent, protoMap map[string]string, sourceMap map[string]discover.ModelSource, nativeModelsMap map[string]string, isConfigurableMap map[string]bool) discover.AgentInfo {
	return discover.AgentInfo{
		Name:           r.Name,
		Category:       r.Category,
		Protocol:       protoMap[r.Name],
		IsConfigurable: isConfigurableMap[r.Name],
		Notes:          nativeModelsMap[r.Name],
	}
}

// dbRecordsForID returns the ProxyRecord(s) addressed by --db <N|all>.
// A numeric value fetches that single record; "all" fetches every record.
// Returns nil (not an error) when no record matches.
func dbRecordsForID(dbFlag string) ([]db.ProxyRecord, error) {
	dbInst, err := db.New()
	if err != nil {
		if strings.Contains(err.Error(), "no such file") || strings.Contains(err.Error(), "file:..") {
			fmt.Printf("提示: 数据库文件不存在，请先运行 proxy sniff / proxy db add 添加代理配置。\n")
			return nil, nil
		}
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	if initErr := dbInst.Init(); initErr != nil {
		_ = dbInst.Close()
		return nil, fmt.Errorf("初始化数据库失败: %w", initErr)
	}
	defer dbInst.Close()

	if strings.EqualFold(dbFlag, "all") {
		records, err := dbInst.List()
		return records, err
	}
	id := parseInt(dbFlag)
	if id <= 0 {
		return nil, fmt.Errorf("--db 的值 %s 无效，必须是整数 ID 或 all", dbFlag)
	}
	record, err := dbInst.GetByID(id)
	if err != nil {
		return nil, fmt.Errorf("查询 DB 记录 %d 失败: %w", id, err)
	}
	if record == nil {
		fmt.Printf("提示: 未找到 DB 记录 ID=%d。\n", id)
		return nil, nil
	}
	return []db.ProxyRecord{*record}, nil
}

// upstreamModelsForProxy fetches live upstream models from a proxy record's
// URL/key. Falls back to the cached models_json in the DB on failure.
// Returns (modelList, sourceLabel, error). sourceLabel is "live" or "cached".
func upstreamModelsForProxy(rec db.ProxyRecord) ([]string, string, error) {
	live := sniff.UpstreamModelList(rec.URL, rec.Key)
	if len(live) > 0 {
		sorted := make([]string, len(live))
		copy(sorted, live)
		sort.Strings(sorted)
		return sorted, "live", nil
	}

	var cached []string
	if rec.ModelsJSON != "" {
		if err := json.Unmarshal([]byte(rec.ModelsJSON), &cached); err != nil {
			return nil, "live", fmt.Errorf("代理 %s (%s) 不可达，且缓存的模型列表无效: %w", rec.URL, rec.DetectedFormat, err)
		}
		sort.Strings(cached)
		return cached, "cached", nil
	}
	return nil, "live", fmt.Errorf("代理 %s (%s) 不可达且无缓存模型列表", rec.URL, rec.DetectedFormat)
}

// ---- AGENT MODELS ----

// agentModelsCmd shows each agent runtime's native model support.
// --agents <name1[,name2,...]|all>; default: all discovered agents.
// The legacy --name / positional argument is deprecated but still works.
var (
	agentModelsAgents string
	agentModelsName   string
)

var agentModelsCmd = &cobra.Command{
	Use:   "models [name]",
	Short: "显示 agent 原生支持的大模型",
	Long: `显示 agent 运行时本身支持的大模型信息。

默认行为：不加 --agents 时显示所有已发现 agent 的模型支持情况。
--agents <名称,名称,...> 仅显示指定 agent；--agents all 显示全部已注册 agent。

输出内容：
  - Agent 名称与类型（CLI / IDE）
  - 协议类型（OpenAI 兼容 / ACP / N/A）
  - 模型来源：自定义模型 / 需重定向 / 自有模型
  - 模型列表：agent 本身可接受的模型名
  - 说明：备注信息

DEPRECATED：旧的 --name / 位置参数 仍可工作，但请使用 --agents。

用法：
  agent-nexus agent models                  显示所有已发现 agent
  agent-nexus agent models --agents all     显示全部已注册 agent
  agent-nexus agent models --agents claude  仅显示 claude
  agent-nexus agent models --agents claude,codex
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		var targetAgents []discover.AgentInfo
		var err error

		// Legacy positional / --name still accepted for backward compat.
		legacy := false
		if len(args) > 0 || agentModelsName != "" {
			legacy = true
			name := agentModelsName
			if len(args) > 0 {
				name = args[0]
			}
			agents, aerr := agentNameFilter(name)
			if aerr != nil {
				return aerr
			}
			targetAgents = agents
		} else {
			if agentModelsAgents != "" {
				targetAgents, err = agentNameFilter(agentModelsAgents)
				if err != nil {
					return err
				}
			} else {
				// default: all discovered agents on the machine.
				targetAgents = discover.Discover()
			}
		}

		if legacy {
			fmt.Println("[提示] 位置参数和 --name 已弃用，请使用 --agents。")
			fmt.Println()
		}

		discover.RenderModelTable(targetAgents)
		return nil
	},
}

// ---- PROXY MODELS ----

var proxyModelsCmd = &cobra.Command{
	Use:   "models --db <N|all>",
	Short: "显示 AI 网关/厂家支持的大模型",
	Long: `显示 AI 网关/厂家支持的大模型列表。模型来自已嗅探并保存到数据库的代理配置
(proxy sniff / proxy db add)，或实时从代理 /v1/models 拉取。

默认行为：不加 --db 时，显示 DB 记录 1 的模型。
--db <N> 显示指定 ID 的代理记录的模型；--db all 显示数据库中所有记录。

输出内容（每条记录）：
  - 记录 ID、URL、检测到的协议格式
  - OpenAI / Anthropic 兼容性
  - 模型数量
  - 模型列表

用法：
  agent-nexus proxy models                 显示 DB 记录 1 的模型
  agent-nexus proxy models --db 1          同上
  agent-nexus proxy models --db 2          显示 DB 记录 2
  agent-nexus proxy models --db all        显示 DB 中所有记录的模型
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		dbFlag := "1"
		if proxyModelsDb != "" {
			dbFlag = proxyModelsDb
		}

		records, err := dbRecordsForID(dbFlag)
		if err != nil {
			return err
		}
		if len(records) == 0 {
			fmt.Println("数据库中没有任何代理配置记录。")
			fmt.Println()
			return nil
		}

		for i, rec := range records {
			if len(records) > 1 {
				fmt.Printf("代理配置 #%d  (共 %d 条):\n", rec.ID, len(records))
			} else {
				fmt.Printf("代理配置 #%d:\n", rec.ID)
			}
			fmt.Printf("  URL:       %s\n", rec.URL)
			fmt.Printf("  格式:      %s\n", rec.DetectedFormat)
			fmt.Printf("  OpenAI:    %v\n", rec.OpenAICap)
			fmt.Printf("  Anthropic: %v\n", rec.AnthropicCap)
			fmt.Printf("  模型数量:  %d\n", rec.ModelCount)

			models, src, aerr := upstreamModelsForProxy(rec)
			if aerr != nil {
				fmt.Printf("  模型列表:  %s\n", aerr.Error())
			} else {
				fmt.Printf("  来源:      %s\n", src)
				fmt.Printf("  模型列表 (%d):\n", len(models))
				for _, m := range models {
					fmt.Printf("    - %s\n", m)
				}
			}
			if i < len(records)-1 {
				fmt.Println(strings.Repeat("-", 60))
			}
		}
		fmt.Println()
		return nil
	},
}

var proxyModelsDb string

func initProxyModels() {
	proxyModelsCmd.Flags().StringVar(&proxyModelsDb, "db", "", "DB 记录 ID（默认 1），或用 all 显示全部")
}

// ---- CONF MODELS ----

// MatchResult describes how one agent's default model aligns with upstream models.
type MatchResult struct {
	AgentName      string
	Category       string
	Protocol       string
	IsConfigurable bool
	DefaultModel   string
	Status         string // matched / unmatched / N/A
	MatchedTo      string // the upstream model it matched, if any
	Notes          string
}

// matchAgentsToUpstream computes how each agent's default model matches the
// upstream models of the given proxy records, printing progress to stdout.
func matchAgentsToUpstream(agents []discover.AgentInfo, recs []db.ProxyRecord) ([]MatchResult, []string) {
	var warnings []string

	upstreamSet := make(map[string]struct{})
	for _, rec := range recs {
		models, src, err := upstreamModelsForProxy(rec)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("DB #%d (%s): %s", rec.ID, rec.URL, err.Error()))
			continue
		}
		fmt.Printf("[conf models] DB #%d (%s): 模型 %d 个 (来源=%s)\n", rec.ID, rec.URL, len(models), src)
		for _, m := range models {
			upstreamSet[m] = struct{}{}
		}
	}

	fmt.Println()
	results := make([]MatchResult, 0, len(agents))
	for _, a := range agents {
		dm, ok := shared.GetDefaultModel(a.Name)
		status := "unmatched"
		matchedTo := "N/A"
		notes := ""

		if !a.IsConfigurable {
			status = "N/A"
			notes = "不可配置，不使用代理模型"
		} else if !ok {
			status = "unmatched"
			notes = "无默认模型配置"
		} else {
			for up := range upstreamSet {
				if strings.EqualFold(up, dm) {
					status = "matched"
					matchedTo = up
					notes = "默认模型在上游中存在"
					break
				}
			}
			if status == "unmatched" {
				notes = "默认模型在上游中不存在，需通过代理模型映射"
			}
		}
		results = append(results, MatchResult{
			AgentName:      a.Name,
			Category:       a.Category,
			Protocol:       a.Protocol,
			IsConfigurable: a.IsConfigurable,
			DefaultModel:   dm,
			Status:         status,
			MatchedTo:      matchedTo,
			Notes:          notes,
		})
	}
	return results, warnings
}

func renderConfModels(results []MatchResult, warnings []string, agentFlag, dbFlag string) {
	if len(warnings) > 0 {
		fmt.Println("警告:")
		for _, w := range warnings {
			fmt.Printf("  - %s\n", w)
		}
		fmt.Println()
	}

	if len(results) == 0 {
		fmt.Println("没有可匹配的 agent。")
		return
	}

	fmt.Printf("Agent 默认模型 ↔ 上游模型 匹配 (agents=%s, db=%s):\n", agentFlag, dbFlag)
	fmt.Println(strings.Repeat("-", 110))

	fmt.Printf("  %-10s  %-4s  %-16s  %-24s  %-12s  %-24s  %s\n",
		"Agent", "类型", "协议", "默认模型", "匹配", "匹配到上游", "说明")
	fmt.Println("  " + strings.Repeat("-", 100))

	for _, r := range results {
		icon := ""
		switch r.Status {
		case "matched":
			icon = "✅ matched"
		case "unmatched":
			icon = "❌ unmatched"
		case "N/A":
			icon = "—  N/A"
		default:
			icon = r.Status
		}
		fmt.Printf("  %-10s  %-4s  %-16s  %-24s  %-12s  %-24s  %s\n",
			r.AgentName, r.Category, r.Protocol, r.DefaultModel,
			icon, r.MatchedTo, r.Notes)
	}
	fmt.Println()
}

var (
	confModelsAgents string
	confModelsDb     string
)

var confModelsCmd = &cobra.Command{
	Use:   "models --agents <name1[,name2,...]|all> --db <N|all>",
	Short: "显示 agent 与 DB 记录中大模型的匹配情况",
	Long: `显示 agent 默认模型与 AI 网关/厂家支持的上游大模型的匹配情况。
用于确认每个 agent 运行时 agent-nexus 写入的默认模型是否能在网关侧解析。

默认行为：不加 flag 时，agents=all（已注册的全部 agent），db=1（首个 DB 记录）。
--agents <名称,名称,...> 筛选 agent；--agents all 全部。
--db <N> 使用指定 DB 记录；--db all 合并所有 DB 记录的上游模型一起匹配。

匹配结果：
  ✅ matched   — agent 默认模型在网关上游中存在
  ❌ unmatched — 不存在，需要通过 proxy 模型映射（model map）
  —  N/A       — 该 agent 不可配置，不使用代理模型

用法：
  agent-nexus conf models                              默认：全部 agent ↔ DB 记录 1
  agent-nexus conf models --agents claude --db 1       claude 运行时 ↔ DB 记录 1
  agent-nexus conf models --agents all --db 2          全部 agent ↔ DB 记录 2
  agent-nexus conf models --agents all --db all        全部 agent ↔ 所有 DB 记录合并
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		agentFlag := confModelsAgents
		dbFlag := confModelsDb
		if agentFlag == "" {
			agentFlag = "all"
		}
		if dbFlag == "" {
			dbFlag = "1"
		}

		agents, err := agentNameFilter(agentFlag)
		if err != nil {
			return err
		}
		records, err := dbRecordsForID(dbFlag)
		if err != nil {
			return err
		}
		if len(records) == 0 {
			fmt.Println("数据库中没有任何代理配置记录，无法匹配。")
			fmt.Println("请先运行 proxy sniff / proxy db add 添加代理配置。")
			fmt.Println()
			return nil
		}

		results, warnings := matchAgentsToUpstream(agents, records)
		renderConfModels(results, warnings, agentFlag, dbFlag)
		return nil
	},
}

// initModelsCmds registers the agent models (now in this file), proxy models,
// and conf models commands, and configures their flags.
func initModelsCmds() {
	agentModelsCmd.Flags().StringVar(&agentModelsAgents, "agents", "", "指定 agent（逗号分隔），用 all 表示全部（可选，默认显示已发现的 agent）")
	agentModelsCmd.Flags().StringVarP(&agentModelsName, "name", "n", "", "[已弃用] 请使用 --agents")

	initProxyModels()

	confModelsCmd.Flags().StringVar(&confModelsAgents, "agents", "", "指定 agent（逗号分隔），用 all 表示全部（默认 all）")
	confModelsCmd.Flags().StringVar(&confModelsDb, "db", "", "DB 记录 ID（默认 1），或用 all 显示全部")
}

// initConfUpstreamModels adds the legacy "conf upstream-models" command for
// backward compatibility. It is preserved verbatim from the original root.go.
func initConfUpstreamModels() {
	confCmd.AddCommand(&cobra.Command{
		Use:   "upstream-models",
		Short: "查询 AI 代理上游模型列表（已弃用，推荐 proxy models）",
		Long: `查询 AI 代理（如 CCX/Desktop）的上游可用模型列表。

该命令用于在自动配置前确认代理当前实际接入的模型。
支持通过全局 --url / --key 指定代理，或使用自动检测。

DEPRECATED: 推荐使用 "proxy models" 查看 DB 中已保存记录的模型。
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := getProxySettings()
			if err != nil || p == nil {
				return fmt.Errorf("未检测到 AI 代理配置: %v\n请使用 --url 和 --key 指定代理，或确保代理正在运行", err)
			}
			fmt.Printf("AI 代理: %s (%s)\n", p.Source, p.BaseURL)
			models := sniff.UpstreamModelList(p.BaseURL, p.APIKey)
			if len(models) == 0 {
				fmt.Println("上游模型列表为空。")
				return nil
			}
			sorted := make([]string, len(models))
			copy(sorted, models)
			sort.Strings(sorted)
			fmt.Printf("上游可用模型 (%d)：\n", len(models))
			fmt.Println(strings.Repeat("-", 60))
			for idx, m := range sorted {
				if idx%4 == 0 {
					fmt.Printf("  ")
				}
				display := m
				if len(display) > 28 {
					display = display[:27] + "."
				}
				fmt.Printf("%-30s", display)
				if idx%4 == 3 {
					fmt.Println()
				}
			}
			fmt.Println()
			return nil
		},
	})
}
