package cmd

import (
	"fmt"
	"strings"

	"agent-nexus/internal/db"
	"github.com/spf13/cobra"
)

type runConfListOpts struct {
	branch string // filter by branch
	agent  string // filter by agent name
	typ    string // filter by type (global|per-agent)
	all    bool   // show all snapshots regardless of filters
}

// confListCmd is the unified list command.
//
// Usage:
//   agent-nexus conf list                    # list all snapshots
//   agent-nexus conf list --branch main       # filter by branch
//   agent-nexus conf list --agent codex       # filter by agent
//   agent-nexus conf list --type global       # filter by type (global|per-agent)
//   agent-nexus conf list --all               # show all snapshots
var confListCmd = &cobra.Command{
	Use:   "list",
	Short: "列出所有配置快照",
	Long: `从 backup_snapshots 表查询所有配置快照，支持按分支、agent、类型过滤。

用法：
  agent-nexus conf list                                列出所有快照
  agent-nexus conf list --branch main                  只显示主分支
  agent-nexus conf list --agent codex                  只显示 codex 相关的快照
  agent-nexus conf list --type global                  只显示全局快照
  agent-nexus conf list --type per-agent               只显示逐 agent 快照
  agent-nexus conf list --branch dev --agent codex     组合过滤
  agent-nexus conf list --all                          显示所有快照

示例：
  agent-nexus conf list
  agent-nexus conf list --branch production
  agent-nexus conf list --type per-agent
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := runConfListOpts{
			branch: cflBranch,
			agent:  cflAgent,
			typ:    cflType,
			all:    cflAll,
		}
		return runConfList(opts)
	},
}

var (
	cflBranch string
	cflAgent  string
	cflType   string
	cflAll    bool
)

func initConfListCmd() {
	flFlags := confListCmd.Flags()
	flFlags.StringVar(&cflBranch, "branch", "", "按分支过滤快照")
	flFlags.StringVar(&cflAgent, "agent", "", "按 agent 名称过滤（逗号分隔，或 'all'）")
	flFlags.StringVar(&cflType, "type", "", "按类型过滤 (global|per-agent)")
	flFlags.BoolVarP(&cflAll, "all", "a", false, "显示所有快照（含所有分支）")
}

func runConfList(opts runConfListOpts) error {
	// Parse agent filter list
	var agentFilter []string
	if !opts.all && opts.agent != "" {
		for _, a := range strings.Split(opts.agent, ",") {
			a = strings.TrimSpace(a)
			if a != "" {
				agentFilter = append(agentFilter, a)
			}
		}
	}

	dbInst, dbErr := db.New()
	if dbErr != nil {
		return fmt.Errorf("数据库不可用（%v），请检查后重试", dbErr)
	}
	defer dbInst.Close()
	if initErr := dbInst.Init(); initErr != nil {
		return fmt.Errorf("数据库初始化失败: %w", initErr)
	}

	dbSnapshots, _ := dbInst.ListSnapshots()

	fmt.Printf("\n配置版本历史\n")
	fmt.Println(strings.Repeat("-", 70))

	displayedCount := 0

	for _, s := range dbSnapshots {
		if !opts.all && opts.branch != "" && s.Branch != opts.branch {
			continue
		}
		if len(agentFilter) > 0 && !opts.all {
			matched := false
			if s.AgentName != "" && s.AgentName != "ALL" {
				for _, filter := range agentFilter {
					if strings.Contains(s.AgentName, filter) {
						matched = true
						break
					}
				}
				if !matched {
					continue
				}
			} else {
				matched = true
			}
		}
		if !opts.all && opts.typ != "" && s.Type != opts.typ {
			continue
		}

		snapshotName := s.Name
		if snapshotName == "" {
			snapshotName = "(未命名)"
		}
		fmt.Printf("\n  名称: %s\n", snapshotName)
		fmt.Printf("  ID: %s | 分支: %s | 类型: %s\n", s.ID, s.Branch, s.Type)

		// Show config file count for this snapshot
		entries, _ := dbInst.GetEntriesBySnapshot(s.ID)
		var hasError bool
		for _, e := range entries {
			if e.Error != "" {
				hasError = true
				break
			}
		}
		if hasError {
			fmt.Printf("    ⚠ 部分文件备份失败\n")
		}
		fmt.Printf("    文件: %d 个\n", len(entries))
		displayedCount++
	}

	if displayedCount == 0 {
		fmt.Println("  未找到匹配条件。使用 'agent-nexus conf backup' 创建快照。")
	}

	fmt.Printf("\n快照总数: %d\n", displayedCount)
	fmt.Println()

	return nil
}
