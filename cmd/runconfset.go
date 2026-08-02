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
	agent  string // "all" or comma-separated agent names
	db     string // "auto" or "<N>" where N is a DB proxy record id; empty means use auto-detect
	dryRun bool
}

var (
	confSetAgent string
	confSetDB    string
	confSetDry   bool
)

// confSetCmd is the single unified entry point for adding/mapping
// AI-gateway models onto agent configurations.
var confSetCmd = &cobra.Command{
	Use:   "set --agent <name|all> [--db auto|<N>]",
	Short: "统一配置入口：从 DB 选取 AI 网关记录，添加/映射大模型到 agent",
	Long: `统一配置入口，收紧为唯一的添加/映射大模型通道。

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
             不传或留空 则回退到自动检测代理（CCX Desktop / CC-Switch）

行为：
  1. 选取 DB 记录 → 获取该记录的 upstream 模型列表
  2. 对每个 agent 分类：
     • 自定义模型 agent（codex/claude/deepseek/opencode/...）→ 自动选取
       最佳匹配的 upstream 模型写入 agent 配置（"添加自定义模型"）
     • 重定向模型 agent（kimi/hermes/qoder/trae）→ 通过代理模型映射，
       将 agent 原生模型名映射到 upstream 模型（"映射"）
       映射算法：关键字匹配 → 默认模型匹配 → 兜底首个 upstream 模型
  3. 将重定向映射写入 proxy_model_mappings 表持久化
  4. 自动备份：调用 conf bak 对将被修改的 agent 配置文件做全量备份
  5. 写入各 agent 配置文件
`,
	RunE: func(cmd *cobra.Command, args []string) error {
		opts := runConfSetOpts{
			agent:  confSetAgent,
			db:     confSetDB,
			dryRun: confSetDry,
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
	fs.StringVar(&confSetDB, "db", "", "AI 网关来源：auto=最小 id 记录，<N>=指定 id；留空则自动检测")
	fs.BoolVarP(&confSetDry, "dry-run", "d", false, "预览模式，不实际写入")
}

// runConfSet is the unified configuration pipeline.
func runConfSet(opts runConfSetOpts) error {
	// 1. Resolve proxy source: DB record (auto/N) or auto-detect
	if opts.db != "" {
		return runConfSetFromDB(opts)
	}

	// DB flag not provided: fallback to auto-detect
	p, src, err := getProxySource(proxyURL, proxyKey)
	if err != nil {
		return fmt.Errorf("获取代理配置失败: %v", err)
	}
	if p == nil {
		return fmt.Errorf(
			"未检测到 AI 代理配置；请使用 --db auto 或 --db <N> 从 DB 选择，\n" +
				"或使用 --url 和 --key 指定代理")
	}
	fmt.Printf("AI 代理来源: %s (%s)\n", src, p.BaseURL)

	// When not using DB, fall back to the legacy resolution flow
	toConfigureNames, err := resolveAgentList(opts.agent)
	if err != nil {
		return err
	}
	if len(toConfigureNames) == 0 {
		fmt.Println("未找到可配置的 agent")
		return nil
	}

	upstreamModels := probeUpstreamModels(p.BaseURL, p.APIKey)

	// Resolve model for each agent (legacy)
	legacyProcess(p, upstreamModels, toConfigureNames, opts.dryRun)
	return nil
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
		BaseURL:  rec.URL,
		APIKey:   rec.Key,
		Source:   proxy.ProxyType("db"),
		ModelMap: make(map[string]string),
	}
	// Build proxy map from stored redirect mappings for this record
	mappings := dbInst.GetAllModelMappingsByProxy(proxyID)
	for _, m := range mappings {
		p.ModelMap[m.NativeModel] = m.UpstreamModel
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
	toConfigureNames, err := resolveAgentList(opts.agent)
	if err != nil {
		return err
	}
	if len(toConfigureNames) == 0 {
		fmt.Println("未找到可配置的 agent")
		return nil
	}

	// Process: custom-model add vs redirect mapping
	_, err = processAgents(p, rec, proxyID, dbInst, upstreamModels, toConfigureNames, opts.dryRun)
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
	// Numeric ID
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

// resolveAgentList parses --agent into a deduped sorted list of agent names.
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
		if !agentInSet(s, configurable) {
			fmt.Printf("⚠ 未知或不可配置 agent: %s（将被跳过）\n", s)
		}
	}
	return names, nil
}

func agentInSet(name string, set []string) bool {
	for _, n := range set {
		if n == name {
			return true
		}
	}
	return false
}

// probeUpstreamModels queries the proxy's /v1/models endpoint.
// Returns nil slice if probe fails.
func probeUpstreamModels(baseURL, apiKey string) []string {
	if baseURL == "" || apiKey == "" {
		return nil
	}
	fmt.Println("正在查询上游模型列表...")
	// sniff.UpstreamModelList requires import
	ids := sniff.UpstreamModelList(baseURL, apiKey)
	fmt.Printf("上游可用模型: %d\n", len(ids))
	return ids
}

// isCustomModelAgent returns true for agents that accept arbitrary upstream
// model names (OpenAI-compatible). These get their best-matched upstream
// model written directly into their config.
func isCustomModelAgent(agentName string) bool {
	customAgents := []string{
		"codex", "claude", "deepseek", "opencode", "openclaw",
		"openclaude", "codebuddy", "lmstudio", "clawx",
	}
	for _, a := range customAgents {
		if a == agentName {
			return true
		}
	}
	return false
}

// processAgents handles the full flow for DB-sourced proxy:
// custom-model adding vs redirect mapping, backup, and config writing.
func processAgents(
	p *proxy.Proxy,
	dbRec *db.ProxyRecord,
	proxyID int,
	dbInst *db.DB,
	upstreamModels []string,
	agentNames []string,
	dryRun bool,
) (configured int, err error) {
	if len(upstreamModels) == 0 {
		return 0, fmt.Errorf("上游无可用模型，无法配置（请先运行 'agent-nexus db add' 或 'agent-nexus proxy sniff' 添加网关记录）")
	}

	fmt.Println("模型分辨率览:")
	fmt.Println(strings.Repeat("-", 80))

	// Classification
	var customAgents []string
	var redirectAgents []string
	for _, name := range agentNames {
		if isCustomModelAgent(name) {
			customAgents = append(customAgents, name)
		} else {
			redirectAgents = append(redirectAgents, name)
		}
	}

	// 1. Custom-model agents: pick best matching upstream model
	for _, name := range customAgents {
		bestModel := model.PickCustomModel(name, upstreamModels)
		defaultModel, _ := shared.GetDefaultModel(name)
		source := "custom"
		notes := fmt.Sprintf("从上游 %d 个模型中自动选取最佳匹配", len(upstreamModels))
		if bestModel == "" {
			fmt.Printf("  %-14s -> (跳过，无匹配模型)\n", name)
			continue
		}
		fmt.Printf("  %-14s -> %-30s [%s] (默认: %s) %s\n",
			name, bestModel, source, defaultModel, notes)
	}

	// 2. Redirect agents: compute redirect mappings
	for _, name := range redirectAgents {
		redirects := model.ComputeRedirectMappings(name, upstreamModels)
		if len(redirects) == 0 {
			fmt.Printf("  %-14s -> (跳过，无法构建映射)\n", name)
			continue
		}
		for _, rm := range redirects {
			fmt.Printf("  %-14s %-25s → %-30s [%s]\n",
				name, rm.NativeModel, rm.UpstreamID, rm.Reason)
		}
	}

	fmt.Println()

	// Dry-run: stop here
	if dryRun {
		fmt.Printf("[预览模式 --dry-run] 未实际写入任何配置。\n")
		return 0, nil
	}

	// 3. Auto-backup BEFORE writing
	fmt.Println("正在备份现有配置...")
	allAgents := discover.Discover()
	nameToAgent := make(map[string]discover.AgentInfo)
	for _, a := range allAgents {
		nameToAgent[a.Name] = a
	}
	snapshotID, bakErr := createAutoBackup(agentNames, nameToAgent, fmt.Sprintf("conf set --agent %v --db %d", agentNames, proxyID))
	if bakErr != nil {
		fmt.Printf("  ⚠ 自动备份失败: %v（继续执行配置写入）\n", bakErr)
	} else {
		fmt.Printf("  备份快照: %s\n", snapshotID)
	}
	fmt.Println()

	// 4. Persist redirect mappings to DB
	for _, name := range redirectAgents {
		redirects := model.ComputeRedirectMappings(name, upstreamModels)
		if len(redirects) == 0 {
			continue
		}
		for _, rm := range redirects {
			pm := &db.ProxyModelMapping{
				ProxyID:       proxyID,
				AgentName:     name,
				NativeModel:   rm.NativeModel,
				UpstreamModel: rm.UpstreamID,
				Reason:        rm.Reason,
				CreatedAt:     time.Now().UTC().Format(time.RFC3339),
			}
			if err := dbInst.UpsertProxyModelMapping(pm); err != nil {
				fmt.Printf("  ⚠ %s 映射写入失败 (%s → %s): %v\n",
					name, rm.NativeModel, rm.UpstreamID, err)
			}
		}
		fmt.Printf("  ✅ %s: 已写入 %d 条重定向映射\n", name, len(redirects))
	}
	if len(redirectAgents) == 0 {
		fmt.Println("  (无重定向 agent，跳过映射)")
	}
	fmt.Println()

	// 5. Write config for custom-model agents
	fmt.Println("正在配置自定义模型 agent...")
	fmt.Println(strings.Repeat("-", 60))
	registry := agent.NewWriterRegistry()
	for _, name := range customAgents {
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
			fmt.Printf("  [FAIL] %s: %v\n", name, err)
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

// legacyProcess runs the pre-DB legacy flow (auto-detect proxy).
func legacyProcess(p *proxy.Proxy, upstreamModels []string, agentNames []string, dryRun bool) {
	resolutions := model.ResolveAllModels(upstreamModels, p.ModelMap)
	fmt.Println("模型分辨率览:")
	fmt.Println(strings.Repeat("-", 80))
	for _, name := range agentNames {
		modelName, found := model.ModelToWrite(resolutions, make(map[string]string), name)
		if !found {
			fmt.Printf("  %-14s -> (未配置)\n", name)
			continue
		}
		notes := ""
		source := ""
		for _, r := range resolutions {
			if r.Agent == name {
				notes = r.Notes
				source = r.Source
				break
			}
		}
		fmt.Printf("  %-14s -> %-30s [%s] %s\n", name, modelName, source, notes)
	}
	fmt.Println()
	if dryRun {
		fmt.Printf("[预览模式 --dry-run] 未实际写入任何配置。\n")
		return
	}
	// Backup
	fmt.Println("正在备份现有配置...")
	bakErr := runConfBackup(runConfBackupOpts{
		agents:  "all",
		branch:  "main",
		message: "conf set: legacy flow",
	})
	if bakErr != nil {
		fmt.Printf("  ⚠ 备份失败: %v\n", bakErr)
	} else {
		fmt.Println("  备份成功")
	}
	fmt.Println()
	// Write configs
	fmt.Println("正在配置 agent...")
	fmt.Println(strings.Repeat("-", 60))
	registry := agent.NewWriterRegistry()
	allAgents := discover.Discover()
	nameToAgent := make(map[string]discover.AgentInfo)
	for _, a := range allAgents {
		nameToAgent[a.Name] = a
	}
	for _, name := range agentNames {
		w := registry.Get(name)
		if w == nil {
			fmt.Printf("  [SKIP] %s: 不支持配置的 writer\n", name)
			continue
		}
		modelName, found := model.ModelToWrite(resolutions, make(map[string]string), name)
		if !found {
			fmt.Printf("  [SKIP] %s: 无法解析模型\n", name)
			continue
		}
		a := nameToAgent[name]
		cfgPath := a.ConfigPath
		if cfgPath == "" {
			home := userHomeDir()
			cfgPath = filepath.Join(home, "."+name, "config.toml")
			_ = os.MkdirAll(filepath.Dir(cfgPath), 0755)
		}
		err := w.Configure(cfgPath, p, modelName)
		if err != nil {
			fmt.Printf("  [FAIL] %s: %v\n", name, err)
			continue
		}
		fmt.Printf("  [OK] %s -> %s\n", name, modelName)
	}
}

// getProxySource resolves a proxy from command-line flags or auto-detect.
// (No longer used for DB; kept for backward compat when --url/--key are set.)
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

// createAutoBackup performs a snapshot of the configs about to be modified.
// It stores the snapshot in BOTH the DB and the filesystem (dual-store) under
// a single UUID, mirroring the behavior of 'conf backup'.
// Returns the snapshot UUID on success, or an error.
func createAutoBackup(
	agentNames []string,
	nameToAgent map[string]discover.AgentInfo,
	message string,
) (string, error) {
	home := userHomeDir()
	destRoot := filepath.Join(home, ".codex", "backups")

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
	snapshotDir := filepath.Join(destRoot, "snapshots", snapshotUUID)

	// Write to filesystem
	if err := os.MkdirAll(snapshotDir, 0755); err != nil {
		return "", fmt.Errorf("创建备份目录失败: %w", err)
	}
	written := 0
	for _, f := range bf {
		dst := filepath.Join(snapshotDir, f.agentName+"_"+f.basename)
		if err := os.WriteFile(dst, f.content, 0644); err != nil {
			return "", fmt.Errorf("写入备份文件失败: %w", err)
		}
		written++
	}

	// Write to DB
	dbInst, dbErr := db.New()
	if dbErr != nil {
		return snapshotUUID, fmt.Errorf("数据库不可用（仅文件系统备份成功）: %w", dbErr)
	}
	defer dbInst.Close()
	if initErr := dbInst.Init(); initErr != nil {
		return snapshotUUID, fmt.Errorf("数据库初始化失败（仅文件系统备份成功）: %w", initErr)
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
	snapshot := &db.BackupSnapshot{
		ID:        snapshotUUID,
		Type:      "global",
		AgentName: "ALL",
		Branch:    "main",
		Message:   message,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := dbInst.CreateSnapshot(snapshot, entries); err != nil {
		return snapshotUUID, fmt.Errorf("写入数据库失败（仅文件系统备份成功）: %w", err)
	}

	fmt.Printf("  备份快照: %s (DB + 文件系统, %d 个文件)\n", snapshotUUID, written)
	return snapshotUUID, nil
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
