package cmd

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agent-nexus/internal/db"
	"agent-nexus/internal/discover"
)

// snapshotPhase identifies the kind of snapshot being taken.
type snapshotPhase string

const (
	phaseManual       snapshotPhase = "manual"       // conf backup: user-triggered read-only snapshot
	phaseBaseline     snapshotPhase = "baseline"     // first-touch: user's state before agent-nexus
	phasePreWrite     snapshotPhase = "pre-write"    // before conf set writes config
	phasePreReset     snapshotPhase = "pre-reset"    // before conf set --reset
	phasePreRestore   snapshotPhase = "pre-restore"  // before conf restore
	phasePostRestore  snapshotPhase = "post-restore" // audit trail after conf restore
)

// SnapshotOpts controls how takeSnapshot builds a snapshot.
type SnapshotOpts struct {
	// AgentNames lists the agent names to back up. Empty = all configurable
	// installed agents discovered at call time.
	AgentNames []string

	// Name is a human-readable label (stored in backup_snapshots.name).
	// Empty = auto-generated from phase + agentNames + timestamp.
	Name string

	// Message is the commit message (stored in backup_snapshots.message).
	Message string

	// Phase selects the snapshot type ("baseline" | "pre-write" | ...).
	// Stored in backup_snapshots.type.
	Phase snapshotPhase

	// Branch defaults to "main" when empty.
	Branch string

	// DryRun previews files without writing to DB.
	DryRun bool

	// ConfigPathOverride is used for restore pre/post snapshots: instead of
	// reading each agent's discovered config path, read exactly this set of
	// absolute paths (used when the file list is known independently of
	// discovery). Empty = use discovery.
	ConfigPathOverride []string
}

// SnapshotResult is returned by takeSnapshot.
type SnapshotResult struct {
	ID   string // snapshot UUID (empty on dry-run or when no files)
	Name string // actual name used
	// Files collected (always populated, even on dry-run).
	Files []ConfigFile
}

// ConfigFile describes one backed-up config file.
type ConfigFile struct {
	AgentName string
	Path      string
	Basename  string
	Content   []byte
	SHA256    string
	ModTime   string
	Error     string
}

// buildName constructs a human-readable snapshot name from the phase and
// agent names, with a unique suffix if the suggested name already exists.
func buildName(phase snapshotPhase, agentNames []string) string {
	ts := time.Now().Format("2006-01-02_15-04-05")
	namesStr := "all"
	if len(agentNames) > 0 {
		namesStr = agentNames[0]
		if len(agentNames) > 1 {
			namesStr = agentNames[0] + "+others"
		}
	}
	return fmt.Sprintf("%s:%s:%s", phase, namesStr, ts)
}

// buildUniqueName appends milliseconds when the suggested name collides with
// an existing snapshot.
func buildUniqueName(suggested string, dbInst *db.DB) string {
	if dbInst == nil {
		return suggested
	}
	if _, err := dbInst.GetSnapshotByName(suggested); err == nil {
		return fmt.Sprintf("%s.%s", suggested, time.Now().Format("150405.000"))
	}
	return suggested
}

// takeSnapshot collects the config files for the given agents and writes a
// snapshot to the DB (unless DryRun).
func takeSnapshot(opts SnapshotOpts) SnapshotResult {
	branch := opts.Branch
	if branch == "" {
		branch = "main"
	}

	// --- Collect files ---
	var files []ConfigFile

	// When ConfigPathOverride is set (pre-restore / post-restore), we read
	// exactly those paths regardless of discovery.
	if len(opts.ConfigPathOverride) > 0 {
		for _, p := range opts.ConfigPathOverride {
			cf := readConfigFile(p, "")
			files = append(files, cf)
		}
	} else {
		allAgents := discover.Discover()
		nameToAgent := make(map[string]discover.AgentInfo)
		for _, a := range allAgents {
			nameToAgent[a.Name] = a
		}

		// Determine which agents to back up.
		var targetNames []string
		if len(opts.AgentNames) > 0 {
			targetNames = opts.AgentNames
		} else {
			targetNames = make([]string, 0, len(nameToAgent))
			for name, a := range nameToAgent {
				if a.HasConfig && a.IsConfigurable && a.ConfigPath != "" {
					targetNames = append(targetNames, name)
				}
			}
		}

		for _, name := range targetNames {
			a, ok := nameToAgent[name]
			if !ok || a.ConfigPath == "" {
				files = append(files, ConfigFile{
					AgentName: name,
					Error:     "未找到配置路径",
				})
				continue
			}
			cf := readConfigFile(a.ConfigPath, name)
			files = append(files, cf)
		}
	}

	// --- Build snapshot name ---
	var agentList []string
	for _, f := range files {
		if f.AgentName != "" {
			agentList = append(agentList, f.AgentName)
		}
	}

	name := opts.Name
	if name == "" {
		name = buildName(opts.Phase, agentList)
	}

	// --- Preview ---
	fmt.Println("快照预览:")
	fmt.Println("------------------------------------------------------------")
	for _, f := range files {
		if f.Error != "" {
			fmt.Printf("  ⚠  %s: %s\n", f.Basename, f.Error)
			continue
		}
		fmt.Printf("  %s  [%s, %d bytes]\n", f.Basename, f.SHA256[:8], len(f.Content))
	}

	if opts.DryRun {
		fmt.Printf("\n[预览模式 --dry-run] 快照未写入数据库。\n")
		return SnapshotResult{Files: files}
	}

	// --- Check name uniqueness against DB (best-effort, DB open is optional) ---
	dbInst, dbErr := db.New()
	if dbErr != nil {
		fmt.Printf("  ⚠ 数据库不可用（%v），快照跳过\n", dbErr)
		return SnapshotResult{Files: files}
	}
	defer dbInst.Close()
	if initErr := dbInst.Init(); initErr != nil {
		fmt.Printf("  ⚠ 数据库初始化失败: %v，快照跳过\n", initErr)
		return SnapshotResult{Files: files}
	}

	name = buildUniqueName(name, dbInst)

	// --- Build entries ---
	var entries []db.BackupConfigEntry
	for _, f := range files {
		entries = append(entries, db.BackupConfigEntry{
			SnapshotID:   "", // set by CreateSnapshotAutoID
			AgentName:    f.AgentName,
			FilePath:     f.Path,
			FileBasename: f.Basename,
			SHA256:       f.SHA256,
			FileSize:     len(f.Content),
			FileContent:  string(f.Content),
			ModTime:      f.ModTime,
			Error:        f.Error,
		})
	}

	// --- Write ---
	snapshotUUID, err := dbInst.CreateSnapshotAutoID(
		string(opts.Phase),
		agentNameArg(entries),
		branch,
		opts.Message,
		name,
		nil,
		entries,
	)
	if err != nil {
		fmt.Printf("  ⚠ 写入快照失败: %v\n", err)
		return SnapshotResult{Files: files}
	}

	fmt.Printf("  快照: %s (名称: %s, %d 个文件)\n", snapshotUUID, name, len(entries))
	return SnapshotResult{ID: snapshotUUID, Name: name, Files: files}
}

// readConfigFile reads a single config file and returns a ConfigFile.
func readConfigFile(path, agentName string) ConfigFile {
	cf := ConfigFile{
		AgentName: agentName,
		Path:      path,
		Basename:  filepath.Base(path),
	}
	data, err := os.ReadFile(path)
	if err != nil {
		cf.Error = err.Error()
		return cf
	}
	info, _ := os.Stat(path)
	hash := sha256.Sum256(data)
	cf.Content = data
	cf.SHA256 = fmt.Sprintf("%x", hash)
	cf.ModTime = info.ModTime().UTC().Format(time.RFC3339)
	return cf
}

// agentNameArg returns a single agent name string for the snapshot row,
// using the first entry's agent name or "ALL".
func agentNameArg(entries []db.BackupConfigEntry) string {
	for _, e := range entries {
		if e.AgentName != "" {
			return e.AgentName
		}
	}
	return "ALL"
}

// ---- Baseline ----

// baselineIfNeeded checks whether an agent needs a baseline snapshot (first
// touch by agent-nexus) and takes one if so.
//
// Trigger condition: agent.HasConfig && !agent.IsConfigured (has a config file
// that doesn't yet contain agent-nexus proxy markers) AND no existing
// baseline snapshot for that agent.
//
// Returns the snapshot UUID if a baseline was taken, "" otherwise.
func baselineIfNeeded(
	agentNames []string,
	nameToAgent map[string]discover.AgentInfo,
	dbInst *db.DB,
	opts SnapshotOpts,
) string {
	// Collect agents that need baseline.
	var need []string
	for _, name := range agentNames {
		a, ok := nameToAgent[name]
		if !ok || !a.HasConfig || a.IsConfigured {
			continue // no config file, or already agent-nexus configured
		}
		// Check if a baseline snapshot for this agent already exists.
		exists, _ := hasBaselineSnapshot(dbInst, name)
		if exists {
			continue
		}
		need = append(need, name)
	}
	if len(need) == 0 {
		return ""
	}

	fmt.Println()
	fmt.Println("首次配置以下 agent，正在保存原始配置快照（baseline）...")
	fmt.Printf("  目标: %s\n", strings.Join(need, ", "))

	message := opts.Message
	if message == "" {
		message = fmt.Sprintf("baseline: 首次配置前原始状态 (%s)", strings.Join(need, ","))
	}

	baseOpts := SnapshotOpts{
		AgentNames: need,
		Name:       "", // auto-generated from buildName
		Message:    message,
		Phase:      phaseBaseline,
		Branch:     "main",
		DryRun:     opts.DryRun,
	}
	result := takeSnapshot(baseOpts)
	return result.ID
}

// hasBaselineSnapshot checks whether any baseline snapshot exists for the
// given agent. Returns (true, nil) if one is found, (false, nil) otherwise.
func hasBaselineSnapshot(dbInst *db.DB, agentName string) (bool, error) {
	snapshots, err := dbInst.ListSnapshots()
	if err != nil {
		return false, err
	}
	for _, s := range snapshots {
		if s.Type != string(phaseBaseline) {
			continue
		}
		// The snapshot's agent_name field stores the first agent in the
		// snapshot; entries cover the rest. For a per-agent baseline the
		// name should match directly; for a multi-agent baseline we check
		// entries.
		if s.AgentName == agentName {
			return true, nil
		}
		entries, eerr := dbInst.GetEntriesBySnapshot(s.ID)
		if eerr != nil {
			continue
		}
		for _, e := range entries {
			if e.AgentName == agentName {
				return true, nil
			}
		}
	}
	return false, nil
}

// ---- Lookup helpers for --reset target resolution ----

// findLatestBaseline returns the most recent baseline snapshot UUID for the
// given agent, or ("", nil) if none exists.
func findLatestBaseline(dbInst *db.DB, agentName string) (string, error) {
	snapshots, err := dbInst.ListSnapshots()
	if err != nil {
		return "", err
	}
	var best string
	bestTime := ""
	for _, s := range snapshots {
		if s.Type != string(phaseBaseline) {
			continue
		}
		if !snapshotContainsAgent(dbInst, s.ID, agentName) {
			continue
		}
		if s.CreatedAt > bestTime {
			bestTime = s.CreatedAt
			best = s.ID
		}
	}
	return best, nil
}

// findLatestPreWrite returns the most recent pre-write snapshot UUID for the
// given agent (the "previous state" to --reset latest).
func findLatestPreWrite(dbInst *db.DB, agentName string) (string, error) {
	snapshots, err := dbInst.ListSnapshots()
	if err != nil {
		return "", err
	}
	var best string
	bestTime := ""
	for _, s := range snapshots {
		if s.Type != string(phasePreWrite) {
			continue
		}
		if !snapshotContainsAgent(dbInst, s.ID, agentName) {
			continue
		}
		if s.CreatedAt > bestTime {
			bestTime = s.CreatedAt
			best = s.ID
		}
	}
	return best, nil
}

// snapshotContainsAgent checks whether a snapshot's entries include the
// given agent name.
func snapshotContainsAgent(dbInst *db.DB, snapshotID string, agentName string) bool {
	entries, err := dbInst.GetEntriesBySnapshot(snapshotID)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.AgentName == agentName {
			return true
		}
	}
	return false
}

// getSnapshotByRef resolves a snapshot reference (UUID, name, "baseline",
// or "latest") to a snapshot ID. Returns ("", nil) when "latest" or
// "baseline" yields no match.
func getSnapshotByRef(dbInst *db.DB, ref string, agentName string) (string, error) {
	switch {
	case ref == "" || strings.EqualFold(ref, "baseline"):
		s, _ := findLatestBaseline(dbInst, agentName)
		return s, nil
	case strings.EqualFold(ref, "latest"):
		s, _ := findLatestPreWrite(dbInst, agentName)
		return s, nil
	default:
		// Try as UUID first, then as name.
		s, _ := dbInst.GetSnapshot(ref)
		if s != nil {
			return s.ID, nil
		}
		s, _ = dbInst.GetSnapshotByName(ref)
		if s != nil {
			return s.ID, nil
		}
		return "", fmt.Errorf("未找到快照 %q", ref)
	}
}
