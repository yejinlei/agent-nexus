package cmd

import (
    "fmt"
    "os"
    "path/filepath"
    "strings"
    "time"

    "agent-nexus/internal/db"
    "agent-nexus/internal/versioning"
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
  - 恢复失败时自动回滚
  - 支持从全局快照中提取单 agent 配置
  - 同时写入 DB 和文件系统
  - 支持恢复文件系统快照（versioning.json）和数据库快照（backup_snapshots）

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
// requested agent names. It matches the agent substring against both the
// original file path and the entry's map key (typically the file basename
// or agent name), so a request for "codex" matches
// "~/.codex/agents/codex/config.json".
func matchEntryByAgent(entryName, filePath string, agentNames []string) bool {
    for _, rn := range agentNames {
        if strings.Contains(entryName, rn) || strings.Contains(filePath, rn) {
            return true
        }
    }
    return false
}

func runConfRestore(opts runConfRestoreOpts, args []string) error {
    home := userHomeDir()
    destRoot := filepath.Join(home, ".codex", "backups")

    targetID := opts.snapshot
    if targetID == "" && len(args) > 0 {
        targetID = args[0]
    }
    if targetID == "" {
        return fmt.Errorf("请指定快照 ID 或名称")
    }

    r := versioning.LoadRegistry(destRoot)

    if strings.EqualFold(targetID, "latest") {
        // Try to find the latest DB snapshot first; fall back to filesystem.
        dbInst, dbErr := db.New()
        var latestDB *db.BackupSnapshot
        if dbErr == nil {
            if initErr := dbInst.Init(); initErr == nil {
                dbSnaps, _ := dbInst.ListSnapshots()
                if len(dbSnaps) > 0 {
                    latestDB = &dbSnaps[0]
                }
                dbInst.Close()
            }
        }
        if latestDB != nil {
            targetID = latestDB.ID
            fmt.Printf("自动选择最新快照（DB）: %s\n", targetID)
        } else {
            latest := r.LatestSnapshot()
            if latest == nil {
                return fmt.Errorf("未找到任何快照")
            }
            targetID = latest.ID
            fmt.Printf("自动选择最新快照（文件系统）: %s\n", targetID)
        }
    }

    // If targetID doesn't look like a snapshot ID (no UUID, no timestamp format),
    // try to resolve it as a human-readable name from the DB.
    if !strings.Contains(targetID, "-") {
        dbInst, dbErr := db.New()
        if dbErr == nil {
            if initErr := dbInst.Init(); initErr == nil {
                dbSnap, snapErr := dbInst.GetSnapshotByName(targetID)
                if snapErr == nil && dbSnap != nil {
                    fmt.Printf("按名称匹配快照: %s → %s\n", dbSnap.Name, dbSnap.ID)
                    targetID = dbSnap.ID
                }
                dbInst.Close()
            }
        }
    }

    s := r.GetSnapshot(targetID)
    if s == nil {
        s, _ = loadSnapshotFromDB(targetID, destRoot)
        if s == nil {
            return fmt.Errorf("快照 %s 不存在", targetID)
        }
        fmt.Printf("快照 %s 来自数据库\n", targetID)
    }

    fmt.Printf("\n恢复到快照: %s (分支: %s)\n", s.ID, s.Branch)
    fmt.Printf("提交信息: %s\n", s.Message)
    fmt.Println(strings.Repeat("-", 60))

    var restoreNames []string
    if opts.agents != "" {
        restoreNames = parseRestoreAgentList(opts.agents)
        fmt.Printf("  仅恢复 agent: %s\n", strings.Join(restoreNames, ", "))
    }

    // Collect the config paths that will be restored (same match logic as
    // the write-back loop below) so the pre-restore snapshot covers exactly
    // the files about to be overwritten.
    configPaths := collectRestorePaths(s, restoreNames)

    preRestoreMessage := fmt.Sprintf("预恢复快照: conf restore %s", targetID)
    if opts.message != "" {
        preRestoreMessage = opts.message
    }

    fmt.Println("创建预恢复快照（恢复前当前文件内容，用于失败回滚）...")
    preSnap, err := r.CreateSnapshot(configPaths, preRestoreMessage, opts.branch, "")
    if err != nil {
        fmt.Printf("  [WARNING] 创建预恢复快照失败: %v\n", err)
    } else {
        fmt.Printf("  预恢复快照: %s (分支: %s)\n", preSnap.ID, preSnap.Branch)
    }

    var restoredFiles []string
    var errors []string

    for name, entry := range s.Configs {
        if entry.Error != "" {
            errors = append(errors, fmt.Sprintf("%s: 未捕获 (%s)", name, entry.Error))
            continue
        }

        if len(restoreNames) > 0 && !matchEntryByAgent(name, entry.FilePath, restoreNames) {
            continue
        }

        if entry.Contents == "" || entry.FilePath == "" {
            errors = append(errors, fmt.Sprintf("%s: 内容为空", name))
            continue
        }

        dir := filepath.Dir(entry.FilePath)
        if err := os.MkdirAll(dir, 0755); err != nil {
            errors = append(errors, fmt.Sprintf("%s: 创建目录失败 %s: %v", name, dir, err))
            continue
        }

        if err := os.WriteFile(entry.FilePath, []byte(entry.Contents), 0644); err != nil {
            errors = append(errors, fmt.Sprintf("%s: 写入失败: %v", name, err))
            continue
        }

        restoredFiles = append(restoredFiles, entry.FilePath)
        fmt.Printf("  ✅ %s → %s\n", name, entry.FilePath)
    }

    dbInst, dbErr := db.New()
    if dbErr == nil {
        defer dbInst.Close()
        _ = dbInst.Init()
        _, _ = dbInst.CreateSnapshotAutoID(
            "global",
            "ALL",
            opts.branch,
            fmt.Sprintf("恢复快照: conf restore %s", targetID),
            fmt.Sprintf("pre-restore-%s", time.Now().Format("2006-01-02_15-04-05")),
            nil,
            nil,
        )
    }

    fmt.Printf("\n✅ 已恢复 %d 个配置文件\n", len(restoredFiles))
    if len(errors) > 0 {
        fmt.Printf("\n⚠ %d 个文件恢复失败:\n", len(errors))
        for _, e := range errors {
            fmt.Printf("  %s\n", e)
        }
        if preSnap != nil {
            fmt.Println("检测到恢复失败，正在回滚到恢复前的文件状态...")
            _, _ = r.RestoreSnapshot(preSnap.ID)
            fmt.Printf("已回滚到预恢复快照: %s\n", preSnap.ID)
        }
        return fmt.Errorf("部分文件恢复失败，已自动回滚")
    }

    fmt.Printf("\n恢复完成。使用 'agent-nexus conf list' 查看版本历史。\n")
    return nil
}

// collectRestorePaths returns the file paths that the write-back loop will
// restore, using the same match logic. These paths are snapshotted before any
// file is overwritten so rollback restores the on-disk pre-restore state.
func collectRestorePaths(s *versioning.Snapshot, restoreNames []string) []string {
    var paths []string
    for name, entry := range s.Configs {
        if entry.Error != "" || entry.Contents == "" || entry.FilePath == "" {
            continue
        }
        if len(restoreNames) > 0 && !matchEntryByAgent(name, entry.FilePath, restoreNames) {
            continue
        }
        paths = append(paths, entry.FilePath)
    }
    return paths
}

// loadSnapshotFromDB reads a snapshot that lives only in the DB and converts
// it into a versioning.Snapshot so the same filesystem restore loop can handle
// both snapshot sources.
func loadSnapshotFromDB(targetID, destRoot string) (*versioning.Snapshot, error) {
    dbInst, dbErr := db.New()
    if dbErr != nil {
        return nil, dbErr
    }
    defer dbInst.Close()
    if initErr := dbInst.Init(); initErr != nil {
        return nil, initErr
    }

    dbSnap, err := dbInst.GetSnapshot(targetID)
    if err != nil || dbSnap == nil {
        return nil, err
    }

    entries, err := dbInst.GetEntriesBySnapshot(targetID)
    if err != nil {
        return nil, err
    }

    var ts time.Time
    if t, parseErr := time.Parse(time.RFC3339, dbSnap.CreatedAt); parseErr == nil {
        ts = t
    }

    s := &versioning.Snapshot{
        ID:        dbSnap.ID,
        Branch:    dbSnap.Branch,
        Message:   dbSnap.Message,
        CreatedAt: ts,
        Configs:   make(map[string]versioning.ConfigEntry),
    }

    for _, e := range entries {
        key := e.FileBasename
        if key == "" {
            key = e.AgentName
        }
        s.Configs[key] = versioning.ConfigEntry{
            FilePath: e.FilePath,
            Contents: e.FileContent,
            SHA256:   e.SHA256,
            Bytes:    e.FileSize,
            Error:    e.Error,
        }
    }

    return s, nil
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

// extractAgentName infers an agent name from a config file path by checking
// for known agent keywords. Used by conf migrate when importing legacy snapshots.
func extractAgentName(path string) string {
    base := filepath.Base(path)
    ext := filepath.Ext(base)
    name := strings.TrimSuffix(base, ext)
    for _, known := range []string{"codex", "claude", "kimi", "opencode", "openclaw", "cursor", "hermes", "gemini", "openclaude"} {
        if strings.Contains(strings.ToLower(name), known) {
            return known
        }
    }
    return name
}
