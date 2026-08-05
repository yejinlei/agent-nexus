package cmd

import (
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

type runConfRestoreOpts struct {
	snapshot string
	agents   string
	branch   string
	message  string
}

var confRestoreCmd = &cobra.Command{
	Use:   "restore <snapshot-id>",
	Short: "从指定快照恢复配置文件",
	Long: `从指定的历史快照恢复 agent 配置文件。

功能：
  - 自动创建预恢复快照（安全网）
  - 支持从全局快照中提取单 agent 配置
  - 所有快照统一存储在 DB

用法：
  agent-nexus conf restore <snapshot-id>
  agent-nexus conf restore --snapshot latest
  agent-nexus conf restore --agents codex,claude <id>
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := runConfRestoreOpts{
			snapshot: crsSnapshot,
			agents:   crsAgents,
			branch:   crsBranch,
			message:  crsMessage,
		}
		return runConfRestore(opts, args)
	},
}

var (
	crsSnapshot string
	crsAgents   string
	crsBranch   string
	crsMessage  string
)

func initConfRestoreCmd() {
	rsFlags := confRestoreCmd.Flags()
	rsFlags.StringVar(&crsSnapshot, "snapshot", "", "要恢复的快照 ID（或 'latest'），可用位置参数代替")
	rsFlags.StringVar(&crsAgents, "agents", "", "仅恢复指定 agent（逗号分隔），留空恢复全部")
	rsFlags.StringVar(&crsBranch, "branch", "main", "预恢复快照所属分支")
	rsFlags.StringVar(&crsMessage, "message", "", "预恢复快照提交信息")
}

// matchEntryByAgent returns true if the config entry belongs to one of the
// requested agent names.
func matchEntryByAgent(entryName, filePath string, agentNames []string) bool {
	for _, rn := range agentNames {
		if strings.Contains(entryName, rn) || strings.Contains(filePath, rn) {
			return true
		}
	}
	return false
}

func runConfRestore(opts runConfRestoreOpts, args []string) error {
	dbInst, dbErr := db.New()
	if dbErr != nil {
		return fmt.Errorf("数据库不可用（%v），请检查后重试", dbErr)
	}
	defer dbInst.Close()
	if initErr := dbInst.Init(); initErr != nil {
		return fmt.Errorf("数据库初始化失败: %w", initErr)
	}

	targetID := opts.snapshot
	if targetID == "" && len(args) > 0 {
		targetID = args[0]
	}
	if targetID == "" {
		return fmt.Errorf("请指定快照 ID 或名称")
	}

	// Resolve "latest"
	if strings.EqualFold(targetID, "latest") {
		dbSnaps, _ := dbInst.ListSnapshots()
		if len(dbSnaps) == 0 {
			return fmt.Errorf("未找到任何快照")
		}
		targetID = dbSnaps[0].ID
		fmt.Printf("自动选择最新快照: %s\n", targetID)
	}

	// Try to resolve by name first
	s, _ := dbInst.GetSnapshot(targetID)
	if s == nil {
		s, _ = dbInst.GetSnapshotByName(targetID)
		if s != nil {
			fmt.Printf("按名称匹配快照: %s → %s\n", s.Name, s.ID)
		}
	}

	if s == nil {
		return fmt.Errorf("快照 %s 不存在", targetID)
	}

	fmt.Printf("\n恢复到快照: %s (分支: %s)\n", s.ID, s.Branch)
	fmt.Printf("提交信息: %s\n", s.Message)
	fmt.Printf("名称: %s\n", s.Name)
	fmt.Println(strings.Repeat("-", 60))

	var restoreNames []string
	if opts.agents != "" {
		restoreNames = parseRestoreAgentList(opts.agents)
		fmt.Printf("  仅恢复 agent: %s\n", strings.Join(restoreNames, ", "))
	}

	// Get entries from DB
	entries, err := dbInst.GetEntriesBySnapshot(s.ID)
	if err != nil {
		return fmt.Errorf("读取快照内容失败: %w", err)
	}
	if len(entries) == 0 {
		return fmt.Errorf("快照 %s 中无配置文件", s.ID)
	}

	// --- Pre-restore snapshot: capture current files, write to DB ---
	preSnapID := ""
	allAgents := discover.Discover()
	nameToAgent := make(map[string]discover.AgentInfo)
	for _, a := range allAgents {
		nameToAgent[a.Name] = a
	}

	// Collect file paths that will be restored (same as write-back loop below)
	preConfigPaths := collectRestorePaths(entries, restoreNames)
	if len(preConfigPaths) > 0 {
		preSnapshotName := fmt.Sprintf("pre-restore-%s", time.Now().Format("2006-01-02_15-04-05.000"))
		preMessage := "预恢复快照（安全网，用于失败回滚）"
		if opts.message != "" {
			preMessage = opts.message
		}
		fmt.Println("创建预恢复快照（恢复前当前文件内容，用于失败回滚）...")
		snapshotUUID, preErr := createPreRestoreSnapshot(preConfigPaths, preMessage, opts.branch, preSnapshotName, nameToAgent, dbInst)
		if preErr != nil {
			fmt.Printf("  [WARNING] 创建预恢复快照失败: %v\n", preErr)
		} else {
			preSnapID = snapshotUUID
			fmt.Printf("  预恢复快照: %s (名称: %s, 分支: %s)\n", snapshotUUID, preSnapshotName, opts.branch)
		}
	}

	// --- Restore: write DB content back to files ---
	var restoredFiles []string
	var restoreErrors []string

	// Sort entries for deterministic output
	sort.Slice(entries, func(i, j int) bool { return entries[i].FileBasename < entries[j].FileBasename })

	for _, e := range entries {
		if e.Error != "" {
			restoreErrors = append(restoreErrors, fmt.Sprintf("%s: 未捕获 (%s)", e.FileBasename, e.Error))
			continue
		}
		if len(restoreNames) > 0 && !matchEntryByAgent(e.FileBasename, e.FilePath, restoreNames) {
			continue
		}
		if e.FileContent == "" || e.FilePath == "" {
			restoreErrors = append(restoreErrors, fmt.Sprintf("%s: 内容为空", e.FileBasename))
			continue
		}

		dir := filepath.Dir(e.FilePath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			restoreErrors = append(restoreErrors, fmt.Sprintf("%s: 创建目录失败 %s: %v", e.FileBasename, dir, err))
			continue
		}
		if err := os.WriteFile(e.FilePath, []byte(e.FileContent), 0644); err != nil {
			restoreErrors = append(restoreErrors, fmt.Sprintf("%s: 写入失败: %v", e.FileBasename, err))
			continue
		}

		restoredFiles = append(restoredFiles, e.FilePath)
		fmt.Printf("  ✅ %s → %s\n", e.FileBasename, e.FilePath)
	}

	// --- Post-restore snapshot (audit trail) ---
	if len(restoredFiles) > 0 {
		_ = createPostRestoreSnapshot(restoredFiles, nameToAgent, targetID, len(restoredFiles), dbInst)
	}

	// --- Rollback on failure ---
	if len(restoredFiles) > 0 && len(restoreErrors) > 0 && preSnapID != "" {
		fmt.Println("检测到恢复失败，正在回滚到恢复前的文件状态...")
		preEntries, preErr := dbInst.GetEntriesBySnapshot(preSnapID)
		rollErrors := 0
		if preErr == nil {
			for _, e := range preEntries {
				if e.FileContent == "" || e.FilePath == "" {
					continue
				}
				dir := filepath.Dir(e.FilePath)
				if mkErr := os.MkdirAll(dir, 0755); mkErr == nil {
					_ = os.WriteFile(e.FilePath, []byte(e.FileContent), 0644)
				}
			}
		} else {
			rollErrors++
		}
		if preErr != nil || rollErrors > 0 {
			fmt.Printf("  ⚠ 回滚部分失败（预恢复快照不可用）\n")
		} else {
			fmt.Printf("已回滚到预恢复快照: %s\n", preSnapID)
		}
	}

	fmt.Printf("\n✅ 已恢复 %d 个配置文件\n", len(restoredFiles))
	if len(restoreErrors) > 0 {
		fmt.Printf("\n⚠ %d 个文件恢复失败:\n", len(restoreErrors))
		for _, e := range restoreErrors {
			fmt.Printf("  %s\n", e)
		}
		return fmt.Errorf("部分文件恢复失败")
	}

	fmt.Printf("\n恢复完成。使用 'agent-nexus conf list' 查看版本历史。\n")
	return nil
}

// createPreRestoreSnapshot captures current on-disk config state into a DB snapshot.
func createPreRestoreSnapshot(
	configPaths []string,
	message, branch, name string,
	nameToAgent map[string]discover.AgentInfo,
	dbInst *db.DB,
) (string, error) {
	var entries []db.BackupConfigEntry
	for _, path := range configPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			entries = append(entries, db.BackupConfigEntry{
				AgentName:    inferAgentName(path, nameToAgent),
				FilePath:     path,
				FileBasename: filepath.Base(path),
				Error:        err.Error(),
			})
			continue
		}
		entries = append(entries, db.BackupConfigEntry{
			AgentName:    inferAgentName(path, nameToAgent),
			FilePath:     path,
			FileBasename: filepath.Base(path),
			FileContent:  string(data),
		})
	}
	return dbInst.CreateSnapshotAutoID("global", "ALL", branch, message, name, nil, entries)
}

// createPostRestoreSnapshot writes an audit snapshot of restored files to DB.
func createPostRestoreSnapshot(
	restoredFiles []string,
	nameToAgent map[string]discover.AgentInfo,
	sourceID string,
	restoreCount int,
	dbInst *db.DB,
) error {
	var entries []db.BackupConfigEntry
	for _, p := range restoredFiles {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		entries = append(entries, db.BackupConfigEntry{
			AgentName:    inferAgentName(p, nameToAgent),
			FilePath:     p,
			FileBasename: filepath.Base(p),
			FileContent:  string(data),
		})
	}
	if len(entries) > 0 {
		_, _ = dbInst.CreateSnapshotAutoID(
			"global", "ALL", "main",
			fmt.Sprintf("恢复快照: conf restore %s（%d 文件）", sourceID, restoreCount),
			fmt.Sprintf("post-restore-%s", time.Now().Format("2006-01-02_15-04-05")),
			nil, entries,
		)
	}
	return nil
}

func collectRestorePaths(entries []db.BackupConfigEntry, restoreNames []string) []string {
	var paths []string
	for _, e := range entries {
		if e.FileContent == "" || e.FilePath == "" {
			continue
		}
		if len(restoreNames) > 0 && !matchEntryByAgent(e.FileBasename, e.FilePath, restoreNames) {
			continue
		}
		paths = append(paths, e.FilePath)
	}
	return paths
}

func parseRestoreAgentList(s string) []string {
	var names []string
	for _, n := range strings.Split(s, ",") {
		n = strings.TrimSpace(n)
		if n != "" {
			names = append(names, n)
		}
	}
	return names
}

// inferAgentName guesses the agent name from a config file path.
func inferAgentName(path string, nameToAgent map[string]discover.AgentInfo) string {
	for name := range nameToAgent {
		if strings.Contains(strings.ToLower(path), strings.ToLower(name)) {
			return name
		}
	}
	return ""
}
