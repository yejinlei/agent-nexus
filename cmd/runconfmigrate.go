package cmd

import (
    "fmt"
    "os"
    "path/filepath"
    "strings"

    "agent-nexus/internal/db"
    "agent-nexus/internal/versioning"
    "github.com/spf13/cobra"
)

var confMigrateCmd = &cobra.Command{
    Use:   "migrate",
    Short: "从 versioning.json 导入配置快照到数据库",
    Long: `将旧的 versioning.json 配置快照导入到新的 DB 存储格式。

功能：
  - 从 ~/.codex/backups/versioning.json 读取所有快照
  - 导入到 backup_snapshots 和 backup_config_entries 表
  - 幂等：重复执行不会创建重复条目
  - --dry-run 预览模式，仅显示将导入的内容

用法：
  agent-nexus conf migrate
  agent-nexus conf migrate --dry-run
`,
    RunE: func(cmd *cobra.Command, args []string) error {
        return runConfMigrate(cfmDryRun)
    },
}

var cfmDryRun bool

func initConfMigrateCmd() {
    mgFlags := confMigrateCmd.Flags()
    mgFlags.BoolVar(&cfmDryRun, "dry-run", false, "预览模式，不实际导入")
}

func runConfMigrate(dryRun bool) error {
    home := userHomeDir()
    destRoot := filepath.Join(home, ".codex", "backups")
    versioningPath := filepath.Join(destRoot, "versioning.json")

    if _, err := os.Stat(versioningPath); os.IsNotExist(err) {
        fmt.Printf("\n未找到 versioning.json 文件: %s\n", versioningPath)
        fmt.Println("没有需要导入的数据。")
        return nil
    }

    r := versioning.LoadRegistry(destRoot)
    snapshots := r.ListSnapshots()

    if len(snapshots) == 0 {
        fmt.Println("\nversioning.json 中没有快照数据，无需导入。")
        return nil
    }

    fmt.Printf("\n正在分析 versioning.json... (%d 个快照)\n", len(snapshots))
    fmt.Println(strings.Repeat("-", 60))

    dbInst, dbErr := db.New()
    if dbErr != nil {
        return fmt.Errorf("无法打开数据库: %v", dbErr)
    }
    defer dbInst.Close()

    if err := dbInst.Init(); err != nil {
        return fmt.Errorf("初始化数据库失败: %v", err)
    }

    var importedCount int
    var skippedCount int

    for _, s := range snapshots {
        existing, _ := dbInst.GetSnapshot(s.ID)
        if existing != nil {
            skippedCount++
            continue
        }

        var entries []db.BackupConfigEntry
        for _, entry := range s.Configs {
            entries = append(entries, db.BackupConfigEntry{
                SnapshotID:   s.ID,
                AgentName:    extractAgentName(entry.FilePath),
                FilePath:     entry.FilePath,
                FileBasename: filepath.Base(entry.FilePath),
                SHA256:       entry.SHA256,
                FileSize:     entry.Bytes,
                FileContent:  entry.Contents,
                ModTime:      entry.ModifiedAt.Format("2006-01-02T15:04:05Z"),
                Error:        entry.Error,
            })
        }

        if dryRun {
            fmt.Printf("  将导入快照: %s (分支: %s, 文件: %d)\n", s.ID, s.Branch, len(entries))
            continue
        }

        snapshotType := "global"
        agentName := "ALL"

        if err := dbInst.CreateSnapshot(&db.BackupSnapshot{
            ID:        s.ID,
            Type:      snapshotType,
            AgentName: agentName,
            Branch:    s.Branch,
            Message:   s.Message,
            CreatedAt: s.CreatedAt.Format("2006-01-02T15:04:05Z"),
        }, entries); err != nil {
            if strings.Contains(err.Error(), "UNIQUE") || strings.Contains(err.Error(), "constraint") {
                skippedCount++
                continue
            }
            fmt.Printf("  [WARNING] 导入快照 %s 失败: %v\n", s.ID, err)
            continue
        }

        importedCount++
        fmt.Printf("  ✅ 导入: %s (%d 个文件)\n", s.ID, len(entries))
    }

    fmt.Printf("\n迁移完成: %d 个导入, %d 个已存在（跳过）\n", importedCount, skippedCount)
    if dryRun {
        fmt.Println("\n运行不带 --dry-run 以实际导入")
    }
    return nil
}
