package cmd

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"agent-nexus/internal/db"
	"agent-nexus/internal/discover"
	"github.com/spf13/cobra"
)

type runConfBackupOpts struct {
	agents  string
	branch  string
	message string
	dryRun  bool
}

var confBackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "手动备份所有/指定 agent 的配置文件（只读快照）",
	Long: `
创建指定 agent 配置文件的只读快照。

行为：
- 只读快照，不写入任何配置
- 写入 DB（backup_snapshots + backup_config_entries）和文件系统（~/.codex/backups/snapshots/<id>/）
- 使用统一 UUID 快照 ID，DB 与文件系统互相可追溯
- --dry-run 预览模式，仅列出将被备份的文件及 SHA256，不实际写入

--agents:
- 默认 all（备份所有已安装且可配置的 agent）
- 逗号分隔的 agent 名称列表，如 "codex,claude"

示例:
  agent-nexus conf backup --dry-run --agents all
  agent-nexus conf backup --agents codex,claude --message "备份前快照"
  agent-nexus conf backup --branch staging --message "staging 分支快照"
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := runConfBackupOpts{
			agents:  cbAgents,
			branch:  cbBranch,
			message: cbMessage,
			dryRun:  cbDryRun,
		}
		return runConfBackup(opts)
	},
}

var (
	cbAgents  string
	cbBranch  string
	cbMessage string
	cbDryRun  bool
)

func initConfBackupCmd() {
	bpFlags := confBackupCmd.Flags()
	bpFlags.StringVarP(&cbAgents, "agents", "a", "all", "要备份的 agent（逗号分隔），用 all 备份所有已安装且可配置的 agent")
	bpFlags.StringVarP(&cbBranch, "branch", "b", "main", "快照所属分支名称")
	bpFlags.StringVarP(&cbMessage, "message", "m", "", "快照提交信息")
	bpFlags.BoolVarP(&cbDryRun, "dry-run", "d", false, "预览模式，不实际写入")
}

// runConfBackup creates a read-only snapshot of selected agent configs,
// writing to both the DB and the filesystem under a single UUID snapshot ID.
func runConfBackup(opts runConfBackupOpts) error {
	home := userHomeDir()
	destRoot := filepath.Join(home, ".codex", "backups")

	fmt.Println("[1/4] 扫描已安装 agent...")
	allAgents := discover.Discover()
	fmt.Printf("  发现 %d 个 agent\n", len(allAgents))

	selectedNames := resolveBackupAgentList(opts.agents, allAgents)
	fmt.Printf("  目标 agent: %s\n", strings.Join(selectedNames, ", "))

	nameToAgent := map[string]discover.AgentInfo{}
	for _, a := range allAgents {
		nameToAgent[a.Name] = a
	}

	fmt.Println("[2/4] 读取配置...")

	type backupFile struct {
		agentName string
		path      string
		basename  string
		content   []byte
		sha256    string
		modTime   string
		error     string
	}

	var bf []backupFile
	for _, name := range selectedNames {
		a, ok := nameToAgent[name]
		if !ok {
			fmt.Printf("  ⚠ %s: 未检测到该 agent，跳过\n", name)
			bf = append(bf, backupFile{agentName: name, error: "未检测到该 agent"})
			continue
		}
		if !a.HasConfig {
			fmt.Printf("  ⚠ %s: 未安装，跳过\n", name)
			bf = append(bf, backupFile{agentName: name, error: "未安装"})
			continue
		}
		if !a.IsConfigurable {
			continue
		}

		cfgPath := a.ConfigPath
		if cfgPath == "" {
			continue
		}
		data, err := os.ReadFile(cfgPath)
		if err != nil {
			bf = append(bf, backupFile{
				agentName: a.Name,
				path:      cfgPath,
				basename:  filepath.Base(cfgPath),
				error:     err.Error(),
			})
			continue
		}
		hash := sha256.Sum256(data)
		info, _ := os.Stat(cfgPath)
		bf = append(bf, backupFile{
			agentName: a.Name,
			path:      cfgPath,
			basename:  filepath.Base(cfgPath),
			content:   data,
			sha256:    fmt.Sprintf("%x", hash),
			modTime:   info.ModTime().UTC().Format(time.RFC3339),
		})
	}

	snapshotType := "global"
	if !strings.EqualFold(opts.agents, "all") {
		snapshotType = "per-agent"
	}

	snapshotMessage := opts.message
	if snapshotMessage == "" {
		snapshotMessage = fmt.Sprintf("备份快照: conf backup --agents %s", opts.agents)
	}

	fmt.Println("[3/4] 备份预览:")
	fmt.Println(strings.Repeat("-", 60))
	for _, f := range bf {
		if f.error != "" {
			fmt.Printf("  ⚠  %s: %s\n", f.basename, f.error)
			continue
		}
		fmt.Printf("  %s  [%s, %d bytes]\n", f.basename, f.sha256[:8], len(f.content))
	}

	// Skip empty snapshots: no agent configs were collected successfully
	validCount := 0
	for _, f := range bf {
		if f.error == "" && len(f.content) > 0 {
			validCount++
		}
	}
	if validCount == 0 {
		fmt.Println("[4/4] 无 agent 配置文件可备份，跳过空快照。")
		return nil
	}
	if opts.dryRun {
		fmt.Printf("\n[预览模式 --dry-run]")
		fmt.Printf("\n快照将写入:\n")
		fmt.Printf("  数据库: backup_snapshots + backup_config_entries\n")
		fmt.Printf("  文件系统: %s/snapshots/<id>/\n", destRoot)
		fmt.Printf("\n运行不带 --dry-run 以实际备份\n")
		return nil
	}

	fmt.Println()
	fmt.Println("[4/4] 写入快照...")

	// Unified snapshot ID source: the UUID returned by the DB write.
	// The filesystem snapshot dir uses the same UUID, keeping DB and
	// filesystem linked by a single ID.

	dbInst, dbErr := db.New()
	if dbErr != nil {
		fmt.Printf("  [WARNING] 数据库不可用: %v\n", dbErr)
		fmt.Println("  将继续写入文件系统备份")
	}

	// Global snapshots get agentName="ALL"; per-agent gets the real agent name.
	agentNameArg := "ALL"
	if snapshotType == "per-agent" {
		for _, f := range bf {
			if f.agentName != "" {
				agentNameArg = f.agentName
				break
			}
		}
	}

	var snapshotUUID string
	if dbErr == nil {
		defer dbInst.Close()
		_ = dbInst.Init()
		var entries []db.BackupConfigEntry
		for _, f := range bf {
			entries = append(entries, db.BackupConfigEntry{
				SnapshotID:   "", // filled by CreateSnapshotAutoID
				AgentName:    f.agentName,
				FilePath:     f.path,
				FileBasename: f.basename,
				SHA256:       f.sha256,
				FileSize:     len(f.content),
				FileContent:  string(f.content),
				ModTime:      f.modTime,
				Error:        f.error,
			})
		}
		var err error
		snapshotUUID, err = dbInst.CreateSnapshotAutoID(snapshotType, agentNameArg, opts.branch, snapshotMessage, nil, entries)
		if err != nil {
			fmt.Printf("  [WARNING] 写入数据库失败: %v\n", err)
		} else {
			fmt.Printf("  数据库快照: %s (分支: %s, 类型: %s)\n", snapshotUUID, opts.branch, snapshotType)
		}
	}

	// Fallback to timestamp ID when DB is unavailable.
	if snapshotUUID == "" {
		snapshotUUID = time.Now().Format("2006-01-02_15-04-05.000000")
	}

	snapshotDir := filepath.Join(destRoot, "snapshots", snapshotUUID)
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		fmt.Printf("  [WARNING] 创建文件系统备份目录失败: %v\n", err)
	} else {
		written := 0
		for _, f := range bf {
			if f.error != "" || len(f.content) == 0 {
				continue
			}
			dst := filepath.Join(snapshotDir, f.agentName+"_"+f.basename)
			if err := os.WriteFile(dst, f.content, 0644); err != nil {
				fmt.Printf("  [WARNING] 写入文件失败: %s: %v\n", f.basename, err)
			} else {
				written++
			}
		}
		fmt.Printf("  文件系统快照: %s (%d 个文件)\n", snapshotDir, written)
	}

	// Removed: redundant backup.Backup() third write.
	// The same data was being written a third time to
	// ~/.codex/backups/agent-configs-<timestamp>/ with a separate
	// timestamp-based ID, producing orphaned data not linkable to the
	// UUID-snapshot in DB or filesystem.  DB + filesystem under one
	// UUID now forms the two consistent copies.

	successCount := 0
	failCount := 0
	for _, f := range bf {
		if f.error != "" {
			failCount++
		} else {
			successCount++
		}
	}
	fmt.Printf("\n备份完成: %d 个成功, %d 个失败\n", successCount, failCount)
	return nil
}

// resolveBackupAgentList resolves --agents to a sorted deduped list.
// "all" means every installed + configurable agent.
func resolveBackupAgentList(agentsStr string, allAgents []discover.AgentInfo) []string {
	if strings.EqualFold(agentsStr, "all") {
		selected := make([]string, 0)
		for _, a := range allAgents {
			if a.HasConfig && a.IsConfigurable {
				selected = append(selected, a.Name)
			}
		}
		sort.Strings(selected)
		return selected
	}

	names := make([]string, 0)
	for _, n := range strings.Split(agentsStr, ",") {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		names = append(names, n)
	}
	seen := make(map[string]bool)
	deduped := make([]string, 0, len(names))
	for _, n := range names {
		if !seen[n] {
			seen[n] = true
			deduped = append(deduped, n)
		}
	}
	sort.Strings(deduped)
	return deduped
}
