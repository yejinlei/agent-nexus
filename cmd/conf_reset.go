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
)

// runConfReset restores each agent to a snapshot of its previous state.
// The target snapshot is resolved from opts.resetTarget:
//   - "baseline" (or empty) → latest baseline snapshot for that agent
//   - "latest" → latest pre-write snapshot for that agent
//   - <uuid>|<name> → snapshot by ID or name
func runConfReset(opts runConfSetOpts) error {
	agentNames, err := resolveAgentList(opts.agent)
	if err != nil {
		return err
	}
	if len(agentNames) == 0 {
		fmt.Println("未找到可配置的 agent")
		return nil
	}

	fmt.Println("正在恢复 agent 到配置快照...")
	fmt.Printf("  目标 agent: %s\n", strings.Join(agentNames, ", "))
	fmt.Println()

	fmt.Println("正在备份当前配置（pre-reset 安全网）...")
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
	preResetName := opts.autoName
	if preResetName == "" {
		preResetName = fmt.Sprintf("pre-reset:%s+%v:%s", agentNames[0], len(agentNames), time.Now().Format("2006-01-02_15-04-05"))
	}
	takeSnapshot(SnapshotOpts{
		AgentNames: allConfigurable,
		Name:       preResetName,
		Message:    "pre-reset: conf set --reset",
		Phase:      phasePreReset,
		DryRun:     opts.dryRun,
	})

	targetRef := opts.resetTarget
	if targetRef == "" {
		targetRef = "baseline"
	}
	fmt.Printf("  恢复目标: %s\n", targetRef)

	if opts.dryRun {
		fmt.Printf("\n[预览模式 --dry-run] 未实际执行恢复。\n")
		return nil
	}

	dbInst, dbErr := db.New()
	if dbErr != nil {
		return fmt.Errorf("打开数据库失败: %w", dbErr)
	}
	defer dbInst.Close()
	if initErr := dbInst.Init(); initErr != nil {
		return fmt.Errorf("初始化数据库失败: %w", initErr)
	}

	fmt.Println()
	fmt.Println(strings.Repeat("-", 60))

	registry := agent.NewWriterRegistry()
	var resetCount int

	for _, name := range agentNames {
		w := registry.Get(name)
		if w == nil {
			fmt.Printf("  [SKIP] %s: 不支持配置的 writer\n", name)
			continue
		}
		if !agent.HasResetter(w) {
			fmt.Printf("  [SKIP] %s: 不支持 reset\n", name)
			continue
		}

		snapID, err := getSnapshotByRef(dbInst, targetRef, name)
		if err != nil {
			fmt.Printf("  [FAIL] %s: %v\n", name, err)
			continue
		}
		if snapID == "" {
			fmt.Printf("  [SKIP] %s: 未找到匹配 %q 的快照\n", name, targetRef)
			continue
		}

		entries, err := dbInst.GetEntriesBySnapshot(snapID)
		if err != nil {
			fmt.Printf("  [FAIL] %s: 读取快照失败: %v\n", name, err)
			continue
		}

		matched := false
		for _, e := range entries {
			if e.AgentName != name {
				continue
			}
			if e.FileContent == "" || e.FilePath == "" {
				fmt.Printf("  [FAIL] %s: 快照中 %s 内容为空\n", name, e.FileBasename)
				matched = true
				continue
			}

			dir := filepath.Dir(e.FilePath)
			if mkErr := os.MkdirAll(dir, 0755); mkErr != nil {
				fmt.Printf("  [FAIL] %s: 创建目录失败: %v\n", name, mkErr)
				continue
			}
			if wrErr := os.WriteFile(e.FilePath, []byte(e.FileContent), 0644); wrErr != nil {
				fmt.Printf("  [FAIL] %s: 写入失败: %v\n", name, wrErr)
				continue
			}
			fmt.Printf("  [OK] %s → 已恢复到快照 %s\n", name, snapID[:8])
			matched = true
			resetCount++
			break
		}

		if !matched {
			fmt.Printf("  [SKIP] %s: 快照 %s 中无该 agent 的条目\n", name, snapID[:8])
		}
	}

	fmt.Println()
	fmt.Printf("恢复完成: %d 个 agent 已恢复\n", resetCount)
	fmt.Printf("使用 'agent-nexus discover' 查看当前状态。\n")
	fmt.Printf("如需查看快照: 'agent-nexus conf list'\n")
	return nil
}