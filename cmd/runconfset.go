package cmd

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"agent-nexus/internal/agent"
	"agent-nexus/internal/db"
	"agent-nexus/internal/discover"
	"agent-nexus/internal/model"
	"agent-nexus/internal/proxy"
	"agent-nexus/internal/shared"
	"agent-nexus/internal/sniff"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// runConfSetOpts carries the unified conf set options.
type runConfSetOpts struct {
	agent        string // "all" or comma-separated agent names
	db           string // "auto" or "<N>" (required)
	dryRun       bool
	autoName     string // user-provided snapshot name for auto-backup
}

var (
	confSetAgent    string
	confSetDB       string
	confSetDry      bool
	confSetAutoName string
)

// confSetCmd is the single unified entry point for writing upstream
// model names onto agent configurations.
var confSetCmd = &cobra.Command{
	Use:   "set --agent <name|all> --db <auto|N>",
	Short: "统一配置入口：从 DB 选取 AI 网关记录，添加大模型到 agent",
	Long: `统一配置入口。

用法：
  agent-nexus conf set --agent all --db auto
  agent-nexus conf set --agent codex --db auto
  agent-nexus conf set --agent kimi,hermes --db 2
  agent-nexus conf set --agent all --db auto --dry-run

参数：
  --agent    指定具体 agent（逗号分隔），用 all 配置所有可配置 agent
  --db       选择 AI 网关记录来源：
	             auto    自动选取 DB 中 id 最小的记录（默认）
	             <N>     选取 DB 中 id=N 的记录
	             必选

行为：
  1. 选取 DB 记录 → 获取该记录的 upstream 模型列表
  2. 对每个 agent 自动选取最佳匹配的 upstream 模型写入 agent 配置
  3. 自动备份：配置写入前对所有已安装 agent 生成全量快照（存 DB）
  4. --backup-name 给自动快照命名；留空自动用时间戳
  5. 写入各 agent 配置文件
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := runConfSetOpts{
			agent:    confSetAgent,
			db:       confSetDB,
			dryRun:   confSetDry,
			autoName: confSetAutoName,
		}
		if opts.agent == "" {
			opts.agent = "all"
		}
		return runConfSet(opts)
	},
}

func initConfSetCmd() {
	fs := confSetCmd.Flags()
	fs.StringVarP(&confSetAgent, "agent", "a", "all", "要配置的 agent（逗号分隔），用 all 配置所有可配置 agent")
	fs.StringVar(&confSetDB, "db", "", "AI 网关来源（必选）：auto=最小 id 记录，<N>=指定 id")
	fs.BoolVarP(&confSetDry, "dry-run", "d", false, "预览模式，不实际写入")
	fs.StringVar(&confSetAutoName, "backup-name", "", "配置前自动快照的名称（留空自动用时间戳）")
	confSetCmd.MarkFlagRequired("db")
}

// runConfSet is the unified configuration pipeline.
func runConfSet(opts runConfSetOpts) error {
	return runConfSetFromDB(opts)
}

// runConfSetFromDB handles the DB-sourced proxy flow.
func runConfSetFromDB(opts runConfSetOpts) error {
	proxyID, err := resolveDBArg(opts.db)
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

	// proxyID == 0 means "auto": pick smallest ID
	var rec *db.ProxyRecord
	if proxyID == 0 {
		rec, err = dbInst.GetMinIDProxy()
		if err != nil {
			return fmt.Errorf("DB 中无 AI 网关记录，请先运行 'agent-nexus db add' 添加记录")
		}
		proxyID = rec.ID
	} else {
		rec, err = dbInst.GetByID(proxyID)
		if err != nil {
			records, listErr := dbInst.List()
			if listErr == nil {
				return fmt.Errorf(
					"DB 记录 %d 不存在（可选 id: %s）", proxyID, idsList(records))
			}
			return fmt.Errorf("DB 记录 %d 不存在: %w", proxyID, err)
		}
	}

	// Build proxy struct from DB record
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

	// Resolve agent list
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

// resolveDBArg parses the --db flag into a proxy record ID.
// "auto" → returns 0 meaning "auto-select minimum ID".
// "<N>" → returns the integer N.
// "" / unrecognised → returns error.
func resolveDBArg(flag string) (int, error) {
	flag = strings.TrimSpace(flag)
	if flag == "" {
		return -1, fmt.Errorf("--db 参数无效（需要 'auto' 或数字 id）")
	}
	if strings.EqualFold(flag, "auto") {
		return 0, nil
	}
	n, err := strconv.Atoi(flag)
	if err != nil {
		return -1, fmt.Errorf(
			"--db 参数无效: %q（需要 'auto' 或数字 id）", flag)
	}
	if n < 1 {
		return -1, fmt.Errorf(
			"--db 参数需要 >= 1 的整数（得到 %d）", n)
	}
	return n, nil
}

// resolveAgentList parses --agent into a sorted list of agent names.
// Returns only agents present in shared.DefaultModels (configurable agents).
func resolveAgentList(agentsStr string) ([]string, error) {
	configurable := make([]string, 0, len(shared.DefaultModels))
	for name := range shared.DefaultModels {
		configurable = append(configurable, name)
	}
	sort.Strings(configurable)

	if strings.EqualFold(agentsStr, "all") {
		return configurable, nil
	}

	selected := strings.Split(agentsStr, ",")
	selectedSet := make(map[string]bool)
	var names []string
	for _, s := range selected {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		selectedSet[s] = true
	}

	for _, n := range configurable {
		if selectedSet[n] {
			names = append(names, n)
		}
	}

	// Warn about unknown agents
	for s := range selectedSet {
		if !containsString(s, configurable) {
			fmt.Printf("⚠ 未知或不可配置 agent: %s（将被跳过）\n", s)
		}
	}
	return names, nil
}

// probeUpstreamModels queries the proxy's /v1/models endpoint.
// Returns nil slice if probe fails.
func probeUpstreamModels(baseURL, apiKey string) []string {
	if baseURL == "" || apiKey == "" {
		return nil
	}
	fmt.Println("正在查询上游模型列表...")
	ids := sniff.UpstreamModelList(baseURL, apiKey)
	fmt.Printf("上游可用模型: %d\n", len(ids))
	return ids
}

// processAgents handles the full flow for DB-sourced proxy:
// model selection, backup, and config writing.
func processAgents(
	p *proxy.Proxy,
	dbRec *db.ProxyRecord,
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

	// Preview: pick best matching upstream model for each agent
	for _, name := range agentNames {
		bestModel := model.PickCustomModel(name, upstreamModels)
		defaultModel, _ := shared.GetDefaultModel(name)
		if bestModel == "" {
			fmt.Printf("  %-14s -> (跳过，无匹配模型)\n", name)
			continue
		}
		fmt.Printf("  %-14s -> %-30s (默认: %s, 上游 %d 个模型中最佳匹配)\n",
			name, bestModel, defaultModel, len(upstreamModels))
	}

	fmt.Println()

	// Dry-run: stop here
	if dryRun {
		fmt.Printf("[预览模式 --dry-run] 未实际写入任何配置。\n")
		return 0, nil
	}

	// Auto-backup BEFORE writing - snapshot ALL configurable agents
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
	autoSnapshotName := autoName
	if autoSnapshotName == "" {
		autoSnapshotName = fmt.Sprintf("auto-backup-%s", time.Now().Format("2006-01-02_15-04-05"))
	}
	// Guard against a duplicate name: backup_snapshots(name) has a UNIQUE
	// constraint and createAutoSnapshot() would fail (and abort the whole
	// conf set) if we hit it. Detect and rename before writing.
	dbCheck, dbCheckErr := db.New()
	if dbCheckErr == nil {
		_ = dbCheck.Init()
		if snap, _ := dbCheck.GetSnapshotByName(autoSnapshotName); snap != nil {
			fmt.Printf("  ⚠ 快照名称 %q 已存在（id: %s），自动备份跳过以避免冲突\n",
				autoSnapshotName, snap.ID)
			autoSnapshotName = fmt.Sprintf("auto-backup-%s", time.Now().Format("2006-01-02_15-04-05.000"))
		}
		dbCheck.Close()
	}
	snapshotUUID, bakErr := createAutoSnapshot(allConfigurable, nameToAgent, autoSnapshotName, fmt.Sprintf("conf set --agent %v --db %d", agentNames, proxyID))
	if bakErr != nil {
		fmt.Printf("  ⚠ 自动备份失败: %v（继续执行配置写入）\n", bakErr)
	} else {
		fmt.Printf("  备份快照: %s (名称: %s)\n", snapshotUUID, autoSnapshotName)
	}
	fmt.Println()

	// Write config for each agent
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
		err := w.Configure(cfgPath, p, bestModel)
		if err != nil {
			if agent.IsProtocolIncompatible(err) {
				pi := err.(*agent.ErrProtocolIncompatible)
				fmt.Printf("  [INCOMPAT] %s: %s\n", name, pi.Reason)
				fmt.Printf("            换用支持 %s 的代理（如 CCX Desktop），或使用其他 agent（如 claude）\n", pi.Reason)
				continue
			}
			fmt.Printf("  [FAIL] %s: %v\n", name, err)
			_ = err // captured for return
			continue
		}
		fmt.Printf("  [OK] %s -> %s\n", name, bestModel)
		configured++
	}
	fmt.Println()

	fmt.Printf("配置完成: %d 个成功\n", configured)
	fmt.Printf("下次运行 'agent-nexus conf set' 将检测到这些配置。\n")
	return configured, nil
}

// createAutoSnapshot creates a DB-only snapshot of the configs about to be modified.
// Backs up all configurable agents, not just those being configured.
// Returns the snapshot UUID on success, or an error.
func createAutoSnapshot(
	agentNames []string,
	nameToAgent map[string]discover.AgentInfo,
	snapshotName string,
	message string,
) (string, error) {

	var bf []backupFile
	for _, name := range agentNames {
		a, ok := nameToAgent[name]
		if !ok || !a.HasConfig || !a.IsConfigurable {
			continue
		}
		if a.ConfigPath == "" {
			continue
		}
		data, err := os.ReadFile(a.ConfigPath)
		if err != nil {
			continue
		}
		hash := sha256.Sum256(data)
		info, _ := os.Stat(a.ConfigPath)
		bf = append(bf, backupFile{
			agentName: name,
			path:      a.ConfigPath,
			basename:  filepath.Base(a.ConfigPath),
			content:   data,
			sha256:    fmt.Sprintf("%x", hash),
			modTime:   info.ModTime().UTC().Format(time.RFC3339),
		})
	}

	if len(bf) == 0 {
		fmt.Println("  无现有配置文件可备份（新配置写入将直接生效）")
		return "", nil
	}

	fmt.Println(strings.Repeat("-", 60))
	for _, f := range bf {
		fmt.Printf("  %s  [%s, %d bytes]\n", f.basename, f.sha256[:8], len(f.content))
	}

	snapshotUUID := uuid.New().String()
	dbInst, dbErr := db.New()
	if dbErr != nil {
		return "", fmt.Errorf("数据库不可用: %w", dbErr)
	}
	defer dbInst.Close()
	if initErr := dbInst.Init(); initErr != nil {
		return "", fmt.Errorf("数据库初始化失败: %w", initErr)
	}
	var entries []db.BackupConfigEntry
	for _, f := range bf {
		entries = append(entries, db.BackupConfigEntry{
			SnapshotID:   snapshotUUID,
			AgentName:    f.agentName,
			FilePath:     f.path,
			FileBasename: f.basename,
			SHA256:       f.sha256,
			FileSize:     len(f.content),
			FileContent:  string(f.content),
			ModTime:      f.modTime,
			Error:        "",
		})
	}
	writtenUUID, err := dbInst.CreateSnapshotAutoID("global", "ALL", "main", message, snapshotName, nil, entries)
	if err != nil {
		return "", fmt.Errorf("写入数据库失败: %w", err)
	}

	fmt.Printf("  备份快照: %s (DB, 名称: %s, %d 个文件)\n", writtenUUID, snapshotName, len(bf))
	return writtenUUID, nil
}

type backupFile struct {
	agentName string
	path      string
	basename  string
	content   []byte
	sha256    string
	modTime   string
}

// idsList returns a comma-separated string of proxy IDs.
func idsList(records []db.ProxyRecord) string {
	var parts []string
	for _, r := range records {
		parts = append(parts, fmt.Sprintf("%d", r.ID))
	}
	return strings.Join(parts, ", ")
}

// parseModelsStr splits a comma-separated "agent=模型名" string into a map.
func parseModelsStr(input string) map[string]string {
	m := make(map[string]string)
	for _, pair := range strings.Split(input, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			continue
		}
		agent := strings.TrimSpace(parts[0])
		model := strings.TrimSpace(parts[1])
		if agent != "" && model != "" {
			m[agent] = model
		}
	}
	return m
}

// containsString checks if name is in set.
func containsString(name string, set []string) bool {
	for _, n := range set {
		if n == name {
			return true
		}
	}
	return false
}

// getProxySource resolves a proxy from command-line flags or auto-detect.
// Returns the source label ("flags" or "auto-detect").
func getProxySource(cliURL, cliKey string) (*proxy.Proxy, string, error) {
	if cliURL != "" || cliKey != "" {
		p, err := proxy.FromFlags(cliURL, cliKey)
		if err != nil {
			return nil, "flags", err
		}
		if p != nil {
			return p, "flags", nil
		}
	}
	p, err := proxy.Detect()
	if err == nil && p != nil {
		return p, "auto-detect", nil
	}
	return nil, "auto-detect", err
}
