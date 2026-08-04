package cmd

import (
    "fmt"
    "path/filepath"
    "strings"

    "agent-nexus/internal/db"
    "agent-nexus/internal/versioning"
    "github.com/spf13/cobra"
)

type runConfListOpts struct {
    branch string // filter by branch
    agent  string // filter by agent name
    typ    string // filter by type (global|per-agent)
    all    bool   // show all snapshots regardless of filters
}

// confListCmd is the unified list command (replaces conf history).
//
// Usage:
//   agent-nexus conf list                    # list all snapshots
//   agent-nexus conf list --branch main       # filter by branch
//   agent-nexus conf list --agent codex       # filter by agent
//   agent-nexus conf list --type global       # filter by type (global|per-agent)
//   agent-nexus conf list --all               # show all snapshots
//
// Deprecated equivalent:
//   agent-nexus conf history  ->  agent-nexus conf list
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
  agent-nexus conf list --all                          显示所有快照（含所有分支）

示例：
  agent-nexus conf list
  agent-nexus conf list --branch production
  agent-nexus conf list --type per-agent
  agent-nexus conf list --all`,
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
    flFlags.BoolVarP(&cflAll, "all", "a", false, "显示所有快照（含所有分支和所有来源）")
}

func runConfList(opts runConfListOpts) error {
    home := userHomeDir()
    destRoot := filepath.Join(home, ".codex", "backups")

    // Parse agent filter list
    var agentFilter []string
    if !opts.all && opts.agent != "" {
        if strings.EqualFold(opts.agent, "all") {
            agentFilter = nil // no filter
        } else {
            for _, a := range strings.Split(opts.agent, ",") {
                a = strings.TrimSpace(a)
                if a != "" {
                    agentFilter = append(agentFilter, a)
                }
            }
        }
    }

    fmt.Printf("\n配置版本历史\n")
    fmt.Println(strings.Repeat("-", 70))

    // Gather snapshots from filesystem (versioning.json)
    r := versioning.LoadRegistry(destRoot)
    fileSnapshots := r.ListSnapshots()

    // Gather snapshots from DB
    var dbSnapshots []db.BackupSnapshot
    dbInst, dbErr := db.New()
    if dbErr == nil {
        defer dbInst.Close()
        _ = dbInst.Init()
        dbSnapshots, _ = dbInst.ListSnapshots()
    }

    // Merge and display
    displayedCount := 0

    // Display filesystem snapshots
    for _, s := range fileSnapshots {
        // Apply filters
        if !opts.all && opts.branch != "" && s.Branch != opts.branch {
            continue
        }
        if len(agentFilter) > 0 {
            matched := false
            for name := range s.Configs {
                for _, filter := range agentFilter {
                    if strings.Contains(name, filter) {
                        matched = true
                        break
                    }
                }
            }
            if !matched {
                continue
            }
        }
        // Type filter for file snapshots: all file-based versioning.json snapshots are "global" type
        if !opts.all && opts.typ != "" {
            if !strings.EqualFold(opts.typ, "global") {
                continue
            }
        }

        displaySnapshot(s, "filesystem")
        displayedCount++
    }

    // Display DB snapshots
    for _, s := range dbSnapshots {
        if !opts.all && opts.branch != "" && s.Branch != opts.branch {
            continue
        }
        if len(agentFilter) > 0 && !opts.all {
            matched := false
            // DB snapshots have agent_name field
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
                // Global snapshot (AgentName == "ALL" or "") - include in per-agent filter
                matched = true
            }
        }
        if !opts.all && opts.typ != "" && s.Type != opts.typ {
            continue
        }

        // Display the snapshot
			snapshotName := s.Name
			if snapshotName == "" {
				snapshotName = "(未命名)"
			}
			fmt.Printf("\n  名称: %s\n", snapshotName)
			fmt.Printf("  [%s] ID: %s | 分支: %s | 类型: %s\n", "", s.ID, s.Branch, s.Type)
        displayedCount++
    }

    if displayedCount == 0 {
        fmt.Println("  未找到匹配条件。使用 'agent-nexus conf backup' 创建快照。")
    }

    // Show available branches
    branchNames := r.BranchesList()
    if len(branchNames) > 1 {
        fmt.Printf("\n  可用分支: %s\n", strings.Join(branchNames, ", "))
        fmt.Printf("  当前分支: %s\n", r.CurrentBranch)
    }

    fmt.Printf("\n快照总数: %d\n", displayedCount)
    fmt.Println()

    return nil
}

func displaySnapshot(s *versioning.Snapshot, source string) {
    fmt.Printf("\n  [%s] %s | 分支: %s | 来源: %s\n", "", s.ID, s.Branch, source)
    fmt.Printf("       时间: %s  信息: %s\n", s.CreatedAt.Format("2006-01-02 15:04:05"), s.Message)
    fmt.Printf("       文件 (%d):\n", len(s.Configs))

    for name, entry := range s.Configs {
        if entry.Error != "" {
            fmt.Printf("        ⚠ %s: %s\n", name, entry.Error)
            continue
        }
        fmt.Printf("        %s  [%s, %d bytes]\n", name, entry.SHA256[:8], entry.Bytes)
    }
}
