package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agent-nexus/internal/agent"
	"agent-nexus/internal/db"
	"agent-nexus/internal/discover"
	"agent-nexus/internal/model"
	"agent-nexus/internal/proxy"
	"agent-nexus/internal/shared"

	"github.com/spf13/cobra"
)

// runConfSetOpts carries the unified conf set options.
type runConfSetOpts struct {
	agent        string // "all" or comma-separated agent names
	db           string // "auto" or "<N>" (required unless --reset)
	dryRun       bool
	autoName     string // user-provided snapshot name for auto-backup
	reset        bool   // restore agents to original (pre-configure) state
	resetTarget  string // target for --reset: "baseline" | "latest" | <snapshot-id|name>
}

var (
	confSetAgent       string
	confSetDB          string
	confSetDry         bool
	confSetAutoName    string
	confSetReset       bool
	confSetResetTarget string
)

// confSetCmd is the single unified entry point for writing upstream
// model names onto agent configurations.
var confSetCmd = &cobra.Command{
	Use:   "set --agent <name|all> [--db <auto|N>] [--reset [<baseline|latest|<id>>]]",
	Short: "统一配置入口：设置或恢复 agent 配置",
	Long: `统一配置入口。

用法：
  agent-nexus conf set --agent all --db auto        从 DB 取代理记录，写入所有 agent
  agent-nexus conf set --agent codex --db auto      仅写 codex
  agent-nexus conf set --agent kimi,hermes --db 2   指定 DB 记录
  agent-nexus conf set --agent all --db auto --dry-run   预览，不写入
  agent-nexus conf set --agent opencode --reset     恢复到 opencode 的 baseline（默认）
  agent-nexus conf set --agent opencode --reset latest   恢复到上一个 pre-write 快照
  agent-nexus conf set --agent opencode --reset <id>   恢复到指定快照
  agent-nexus conf set --agent all --reset           所有 agent 恢复到各自 baseline

参数：
  --agent         指定具体 agent（逗号分隔），用 all 配置所有可配置 agent
  --db            选择 AI 网关记录来源（--reset 时无需）：
                      auto   自动选取 DB 中 id 最小的记录（默认）
                      <N>    选取 DB 中 id=N 的记录
  --reset [<ref>] 恢复 agent 配置，<ref> 可选：
                      baseline  恢复到 agent 首次被 agent-nexus 配置前的状态（默认）
                      latest    恢复到该 agent 上一个 pre-write 快照
                      <id>      按 UUID/名称恢复到指定快照
                      (省略)    等同于 baseline
  --dry-run       预览模式，不实际写入
  --backup-name   操作前自动快照的名称（留空自动用时间戳）

行为：
  1. 非 reset 模式：选取 DB 记录 → 获取 upstream 模型 → 写入 agent 配置
  2. reset 模式：恢复到目标快照（默认 baseline）
  3. 写入前自动拍 pre-write 快照；首次触碰 agent 时拍 baseline 快照
  4. 恢复前自动拍 pre-reset 快照作为安全网
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := runConfSetOpts{
			agent:       confSetAgent,
			db:          confSetDB,
			dryRun:      confSetDry,
			autoName:    confSetAutoName,
			reset:       confSetReset,
			resetTarget: confSetResetTarget,
		}
		if opts.agent == "" {
			opts.agent = "all"
		}
		if !opts.reset && opts.db == "" {
			return fmt.Errorf("--db 参数必须指定（auto 或数字 id）；使用 --reset 时无需 --db")
		}
		return runConfSet(opts)
	},
}

func initConfSetCmd() {
	fs := confSetCmd.Flags()
	fs.StringVarP(&confSetAgent, "agent", "a", "all", "要配置的 agent（逗号分隔），用 all 配置所有可配置 agent")
	fs.StringVar(&confSetDB, "db", "", "AI 网关来源（--reset 时无需）：auto=最小 id 记录，<N>=指定 id")
	fs.BoolVarP(&confSetDry, "dry-run", "d", false, "预览模式，不实际写入")
	fs.StringVar(&confSetAutoName, "backup-name", "", "操作前自动快照的名称（留空自动用时间戳）")
	fs.BoolVarP(&confSetReset, "reset", "r", false, "恢复 agent 配置（默认恢复到 baseline）；--reset 时无需 --db")
	fs.StringVar(&confSetResetTarget, "reset-to", "", "恢复到指定快照：baseline / latest / <snapshot-id>")
}

// runConfSet is the unified configuration pipeline.
func runConfSet(opts runConfSetOpts) error {
	if opts.reset {
		return runConfReset(opts)
	}
	return runConfSetFromDB(opts)
}

// runConfSetFromDB handles the DB-sourced proxy flow.
func runConfSetFromDB(opts runConfSetOpts) error {
	dbR, err := resolveDBArg(opts.db)
	if err != nil {
		return err
	}

	dbInst, openErr := db.New()
	if openErr != nil {
		return fmt.Errorf("打开数据库失败: %w", openErr)
	}
	defer dbInst.Close()
	if initErr := dbInst.Init(); initErr != nil {
		return fmt.Errorf("初始化数据库失败: %w", initErr)
	}

	var rec *db.ProxyRecord
	var proxyID int
	if dbR.Mode == "auto" {
		rec, err = dbInst.GetMinIDProxy()
		if err != nil {
			return fmt.Errorf("DB 中无 AI 网关记录，请先运行 'agent-nexus db add' 添加记录")
		}
		proxyID = rec.ID
	} else {
		proxyID = dbR.ID
		rec, err = dbInst.GetByID(proxyID)
		if err != nil {
			records, listErr := dbInst.List()
			if listErr == nil {
				return fmt.Errorf("DB 记录 %d 不存在（可选 id: %s）", proxyID, idsList(records))
			}
			return fmt.Errorf("DB 记录 %d 不存在: %w", proxyID, err)
		}
	}

	models := db.GetModelsFromRecord(rec)
	p := &proxy.Proxy{
		BaseURL: rec.URL,
		APIKey:  rec.Key,
		Source:  proxy.ProxyType("db"),
	}

	src := fmt.Sprintf("db:%d", rec.ID)
	upstreamModels := models
	fmt.Printf("AI 网关来源: %s (%s, %d 模型)\n", src, rec.URL, rec.ModelCount)
	if len(upstreamModels) > 0 {
		fmt.Printf("使用 DB 中存储的 %d 个上游模型\n", len(upstreamModels))
	} else {
		fmt.Println("DB 中无存储模型，尝试探测上游...")
		upstreamModels = probeUpstreamModels(p.BaseURL, p.APIKey)
	}

	agentNames, err := resolveAgentList(opts.agent)
	if err != nil {
		return err
	}
	if len(agentNames) == 0 {
		fmt.Println("未找到可配置的 agent")
		return nil
	}

	_, err = processAgents(p, rec, proxyID, upstreamModels, agentNames, opts.dryRun, opts.autoName)
	return err
}

// processAgents writes upstream models onto each agent config.
func processAgents(
	p *proxy.Proxy,
	_ *db.ProxyRecord,
	proxyID int,
	upstreamModels []string,
	agentNames []string,
	dryRun bool,
	autoName string,
) (configured int, err error) {
	if len(upstreamModels) == 0 {
		return 0, fmt.Errorf("上游无可用模型，无法配置（请先运行 'agent-nexus db add' 或 'agent-nexus proxy sniff' 添加网关记录）")
	}

	fmt.Println("模型分辨率览:")
	fmt.Println(strings.Repeat("-", 80))

	for _, name := range agentNames {
		bestModel := model.PickCustomModel(name, upstreamModels)
		defaultModel, _ := shared.GetDefaultModel(name)
		if bestModel == "" {
			fmt.Printf("  %-14s -> (跳过，无匹配模型)\n", name)
			continue
		}
		fmt.Printf("  %-14s -> %-30s (默认: %s, 上游 %d 个模型中最佳匹配)\n",
			name, bestModel, defaultModel, len(upstreamModels))
		tr := model.ResolveTierModels(name, upstreamModels, nil)
		if tr.Opus != "" || tr.Sonnet != "" || tr.Haiku != "" {
			for _, tier := range []string{"opus", "sonnet", "haiku"} {
				var v string
				switch tier {
				case "opus":
					v = tr.Opus
				case "sonnet":
					v = tr.Sonnet
				case "haiku":
					v = tr.Haiku
				}
				if v == "" {
					continue
				}
				fmt.Printf("          └─ %s -> %s\n", tier, v)
			}
		}
	}

	fmt.Println()

	if dryRun {
		fmt.Printf("[预览模式 --dry-run] 未实际写入任何配置。\n")
		return 0, nil
	}

	fmt.Println("正在备份现有配置...")
	allAgents := discover.Discover()
	nameToAgent := make(map[string]discover.AgentInfo)
	for _, a := range allAgents {
		nameToAgent[a.Name] = a
	}
	allConfigurable := make([]string, 0, len(nameToAgent))
	for name, a := range nameToAgent {
		if a.HasConfig && a.IsConfigurable && a.ConfigPath != "" {
			allConfigurable = append(allConfigurable, name)
		}
	}

	dbForSnapshot, snapErr := db.New()
	if snapErr == nil {
		_ = dbForSnapshot.Init()
		baseUUID := baselineIfNeeded(agentNames, nameToAgent, dbForSnapshot, SnapshotOpts{
			Message: fmt.Sprintf("conf set --agent %v --db %d", agentNames, proxyID),
		})
		if baseUUID != "" {
			fmt.Printf("  baseline 快照已保存\n")
		}
		dbForSnapshot.Close()
	}

	message := fmt.Sprintf("pre-write: conf set --agent %v --db %d", agentNames, proxyID)
	preWriteName := autoName
	if preWriteName == "" {
		preWriteName = fmt.Sprintf("pre-write:%s+%v:%s", agentNames[0], len(agentNames), time.Now().Format("2006-01-02_15-04-05"))
	}
	takeSnapshot(SnapshotOpts{
		AgentNames: allConfigurable,
		Name:       preWriteName,
		Message:    message,
		Phase:      phasePreWrite,
		DryRun:     dryRun,
	})

	fmt.Println()
	fmt.Println("正在配置 agent...")
	fmt.Println(strings.Repeat("-", 60))
	registry := agent.NewWriterRegistry()

	for _, name := range agentNames {
		w := registry.Get(name)
		if w == nil {
			fmt.Printf("  [SKIP] %s: 不支持配置的 writer\n", name)
			continue
		}
		bestModel := model.PickCustomModel(name, upstreamModels)
		if bestModel == "" {
			fmt.Printf("  [SKIP] %s: 无法选取模型\n", name)
			continue
		}
		a := nameToAgent[name]
		cfgPath := a.ConfigPath
		if cfgPath == "" {
			home := userHomeDir()
			cfgPath = filepath.Join(home, "."+name, "config.toml")
			_ = os.MkdirAll(filepath.Dir(cfgPath), 0755)
		}

		var writeErr error
		var writtenLabel string
		tr := model.ResolveTierModels(name, upstreamModels, nil)
		if tw, ok := w.(agent.TieredConfigWriter); ok {
			tiers := map[string]string{
				"default": tr.Default,
				"opus":    tr.Opus,
				"sonnet":  tr.Sonnet,
				"haiku":   tr.Haiku,
			}
			writeErr = tw.ConfigureTiered(cfgPath, p, tiers)
			writtenLabel = tr.Default
			if tr.Opus != "" || tr.Sonnet != "" || tr.Haiku != "" {
				parts := []string{fmt.Sprintf("opus=%s", tr.Opus), fmt.Sprintf("sonnet=%s", tr.Sonnet), fmt.Sprintf("haiku=%s", tr.Haiku)}
				writtenLabel += " [" + strings.Join(parts, ", ") + "]"
			}
		} else {
			writeErr = w.Configure(cfgPath, p, bestModel)
			writtenLabel = bestModel
		}
		if writeErr != nil {
			if agent.IsProtocolIncompatible(writeErr) {
				pi := writeErr.(*agent.ErrProtocolIncompatible)
				fmt.Printf("  [INCOMPAT] %s: %s\n", name, pi.Reason)
				fmt.Printf("            换用支持 %s 的代理（如 CCX Desktop），或使用其他 agent（如 claude）\n", pi.Reason)
				continue
			}
			fmt.Printf("  [FAIL] %s: %v\n", name, writeErr)
			_ = writeErr
			continue
		}
		fmt.Printf("  [OK] %s -> %s\n", name, writtenLabel)
		configured++
	}
	fmt.Println()

	fmt.Printf("配置完成: %d 个成功\n", configured)
	fmt.Printf("下次运行 'agent-nexus conf set' 将检测到这些配置。\n")
	return configured, nil
}