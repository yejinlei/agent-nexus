package cmd

import (
	"fmt"
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
	name    string
	force   bool
}

var confBackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "手动备份所有/指定 agent 的配置文件（只读快照）",
	Long: `创建指定 agent 配置文件的只读快照。

行为：
- 只读快照，不写入任何配置
- 写入 DB（backup_snapshots + backup_config_entries）
- 使用统一 UUID 快照 ID
- --name 给快照命名，恢复时按名称定位
- --force 允许覆盖同名快照
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
			name:    cbName,
			force:   cbForce,
		}
		return runConfBackup(opts)
	},
}

var (
	cbAgents  string
	cbBranch  string
	cbMessage string
	cbDryRun  bool
	cbName    string
	cbForce   bool
)

func initConfBackupCmd() {
	bpFlags := confBackupCmd.Flags()
	bpFlags.StringVarP(&cbAgents, "agents", "a", "all", "要备份的 agent（逗号分隔），用 all 备份所有已安装且可配置的 agent")
	bpFlags.StringVarP(&cbBranch, "branch", "b", "main", "快照所属分支名称")
	bpFlags.StringVarP(&cbMessage, "message", "m", "", "快照提交信息")
	bpFlags.BoolVarP(&cbDryRun, "dry-run", "d", false, "预览模式，不实际写入")
	bpFlags.StringVar(&cbName, "name", "", "快照名称（人类可读，恢复时用此名称定位）")
	bpFlags.BoolVar(&cbForce, "force", false, "允许覆盖同名快照")
}

// runConfBackup creates a read-only snapshot of selected agent configs using
// the shared takeSnapshot infrastructure (cmd/conf_snapshot.go).
func runConfBackup(opts runConfBackupOpts) error {
	fmt.Println("[1/4] 扫描已安装 agent...")
	allAgents := discover.Discover()
	fmt.Printf("  发现 %d 个 agent\n", len(allAgents))

	selectedNames := resolveBackupAgentList(opts.agents, allAgents)
	fmt.Printf("  目标 agent: %s\n", strings.Join(selectedNames, ", "))

	fmt.Println("[2/4] 读取配置...")

	message := opts.message
	if message == "" {
		message = fmt.Sprintf("备份快照: conf backup --agents %s", opts.agents)
	}

	name := opts.name
	if name == "" {
		name = fmt.Sprintf("snapshot-%s", time.Now().Format("2006-01-02_15-04-05"))
		fmt.Printf("  自动快照名称: %s\n", name)
	}

	// --force: delete any existing snapshot with the same name before creating.
	if opts.force && name != "" && !opts.dryRun {
		dbInst, err := db.New()
		if err == nil {
			defer dbInst.Close()
			if initErr := dbInst.Init(); initErr == nil {
				existing, _ := dbInst.GetSnapshotByName(name)
				if existing != nil {
					fmt.Printf("  覆盖同名快照: %s -> %s\n", existing.Name, existing.ID)
					_ = dbInst.DeleteSnapshot(existing.ID)
				}
			}
		}
	}

	result := takeSnapshot(SnapshotOpts{
		AgentNames: selectedNames,
		Name:       name,
		Message:    message,
		Phase:      phaseManual,
		Branch:     opts.branch,
		DryRun:     opts.dryRun,
	})

	if len(result.Files) == 0 {
		fmt.Println("[4/4] 无 agent 配置文件可备份，跳过空快照。")
		return nil
	}

	validCount := 0
	for _, f := range result.Files {
		if f.Error == "" && len(f.Content) > 0 {
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
		if opts.name != "" {
			fmt.Printf("  快照名称: %s\n", opts.name)
		}
		fmt.Printf("\n运行不带 --dry-run 以实际备份\n")
		return nil
	}

	fmt.Println("[4/4] 写入快照完成")
	if result.ID != "" {
		fmt.Printf("  快照名称: %s\n", result.Name)
		fmt.Printf("  数据库快照: %s (分支: %s, 类型: manual)\n", result.ID, opts.branch)
	}
	fmt.Printf("\n备份完成: %d 个成功, %d 个失败\n",
		validCount, len(result.Files)-validCount)
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
