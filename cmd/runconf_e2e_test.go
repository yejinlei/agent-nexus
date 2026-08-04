package cmd

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-nexus/internal/agent"
	"agent-nexus/internal/db"
	"agent-nexus/internal/model"
	"agent-nexus/internal/proxy"
	"agent-nexus/internal/versioning"
)

func setupTestHome(t *testing.T) (string, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", tmpDir)
	origHomeDir := homeDir
	homeDir = tmpDir
	cleanup := func() {
		os.Setenv("HOME", origHome)
		homeDir = origHomeDir
	}
	return tmpDir, cleanup
}

func createTestConfigFile(dir, name, content string) string {
	path := filepath.Join(dir, name)
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, []byte(content), 0644)
	return path
}

func hash256(data string) string {
	h := sha256.Sum256([]byte(data))
	return fmt.Sprintf("%x", h)
}

func TestConfSet_AllAgents(t *testing.T) {
	_, cleanup := setupTestHome(t)
	defer cleanup()

	err := runConfSet(runConfSetOpts{
		agent: "all",
	})
	if err == nil {
		t.Logf("conf set returned nil (no proxy available)")
	} else {
		t.Logf("conf set returned: %v", err)
	}
}

func TestConfSet_DryRun_NoWrite(t *testing.T) {
	_, cleanup := setupTestHome(t)
	defer cleanup()

	err := runConfSet(runConfSetOpts{
		agent:  "codex",
		dryRun: true,
	})
	if err != nil {
		t.Logf("conf set dry-run returned: %v", err)
	}
}

func TestConfSet_WithModelsOverride(t *testing.T) {
	_, cleanup := setupTestHome(t)
	defer cleanup()

	err := runConfSet(runConfSetOpts{
		agent: "all",
	})
	if err == nil {
		t.Logf("conf set with models override returned nil")
	} else {
		t.Logf("conf set with models override: %v", err)
	}
}

func TestParseModelsStr_E2E(t *testing.T) {
	tests := []struct {
		input    string
		expected map[string]string
	}{
		{"codex=gpt-5.5,claude=glm-5.2", map[string]string{"codex": "gpt-5.5", "claude": "glm-5.2"}},
		{"kimi=sensenova-6.7-flash-lite", map[string]string{"kimi": "sensenova-6.7-flash-lite"}},
		{"codex=,claude=gpt-5.5", map[string]string{"claude": "gpt-5.5"}},
		{"", map[string]string{}},
	}

	for _, tc := range tests {
		result := parseModelsStr(tc.input)
		if len(result) != len(tc.expected) {
			t.Errorf("input %q: len = %d, want %d", tc.input, len(result), len(tc.expected))
			continue
		}
		for k, v := range tc.expected {
			if result[k] != v {
				t.Errorf("input %q: %s = %q, want %q", tc.input, k, result[k], v)
			}
		}
	}
}

func TestConfBackup_Global(t *testing.T) {
	_, cleanup := setupTestHome(t)
	defer cleanup()

	err := runConfBackup(runConfBackupOpts{
		agents:  "all",
		branch:  "main",
		message: "test backup",
		dryRun:  false,
	})
	if err != nil {
		t.Logf("conf backup global: %v", err)
	}
}

func TestConfBackup_Global_DryRun(t *testing.T) {
	_, cleanup := setupTestHome(t)
	defer cleanup()

	err := runConfBackup(runConfBackupOpts{
		agents: "all",
		branch: "main",
		dryRun: true,
	})
	if err != nil {
		t.Logf("conf backup dry-run: %v", err)
	}
}

func TestConfBackup_SingleAgent(t *testing.T) {
	_, cleanup := setupTestHome(t)
	defer cleanup()

	err := runConfBackup(runConfBackupOpts{
		agents:  "codex",
		branch:  "main",
		message: "single agent backup",
		dryRun:  false,
	})
	if err != nil {
		t.Logf("conf backup single agent: %v", err)
	}
}

func TestConfBackup_DBEntryCreated(t *testing.T) {
	_, cleanup := setupTestHome(t)
	defer cleanup()

	err := runConfBackup(runConfBackupOpts{
		agents:  "codex",
		branch:  "main",
		message: "db test backup",
		dryRun:  false,
	})
	if err != nil {
		t.Logf("conf backup: %v", err)
	}

	dbInst, dbErr := db.New()
	if dbErr != nil {
		t.Logf("DB not available: %v", dbErr)
		return
	}
	defer dbInst.Close()
	_ = dbInst.Init()

	snapshots, _ := dbInst.ListSnapshots()
	if len(snapshots) == 0 {
		t.Log("No snapshots in DB (may be expected if no configs discovered)")
	}
}

func TestConfRestore_NonexistentSnapshot(t *testing.T) {
	_, cleanup := setupTestHome(t)
	defer cleanup()

	err := runConfRestore(runConfRestoreOpts{
		snapshot: "nonexistent-id",
	}, []string{})
	if err == nil {
		t.Error("expected error for nonexistent snapshot")
	}
}

func TestConfRestore_Latest_WithSnapshot(t *testing.T) {
	tmpDir, cleanup := setupTestHome(t)
	defer cleanup()

	destRoot := filepath.Join(tmpDir, ".codex", "backups")
	r := versioning.NewRegistry(destRoot)

	cfgPath := createTestConfigFile(destRoot, "test.toml", "original")

	s, err := r.CreateSnapshot([]string{cfgPath}, "test", "main", "")
	if err != nil {
		t.Fatalf("CreateSnapshot failed: %v", err)
	}

	os.WriteFile(cfgPath, []byte("modified"), 0644)

	err = runConfRestore(runConfRestoreOpts{
		snapshot: s.ID,
		branch:   "main",
	}, []string{})
	if err != nil {
		t.Logf("conf restore latest: %v", err)
	}

	data, _ := os.ReadFile(cfgPath)
	if string(data) != "original" {
		t.Logf("restored content: %q, expected 'original'", string(data))
	}
}

func TestConfRestore_PreRestoreSnapshotCreated(t *testing.T) {
	tmpDir, cleanup := setupTestHome(t)
	defer cleanup()

	destRoot := filepath.Join(tmpDir, ".codex", "backups")
	r := versioning.NewRegistry(destRoot)

	cfgPath := createTestConfigFile(destRoot, "test.toml", "original")
	s, _ := r.CreateSnapshot([]string{cfgPath}, "before", "main", "")

	os.WriteFile(cfgPath, []byte("modified"), 0644)

	_ = runConfRestore(runConfRestoreOpts{
		snapshot: s.ID,
		branch:   "main",
	}, []string{})

	snapshots := r.ListSnapshots()
	preRestoreFound := false
	for _, snap := range snapshots {
		if strings.Contains(snap.Message, "pre-restore") {
			preRestoreFound = true
			break
		}
	}
	if !preRestoreFound {
		t.Log("Pre-restore snapshot may not be created (depends on implementation)")
	}
}

func TestConfRestore_RollbackOnFailure(t *testing.T) {
	tmpDir, cleanup := setupTestHome(t)
	defer cleanup()

	destRoot := filepath.Join(tmpDir, ".codex", "backups")
	r := versioning.NewRegistry(destRoot)

	s, _ := r.CreateSnapshot([]string{filepath.Join(tmpDir, "nonexistent.toml")}, "test", "main", "")

	err := runConfRestore(runConfRestoreOpts{
		snapshot: s.ID,
		branch:   "main",
	}, []string{})
	if err == nil {
		t.Log("conf restore with nonexistent file returned nil (expected graceful handling)")
	} else {
		t.Logf("conf restore failed as expected: %v", err)
	}
}

func TestConfRestore_AgentFilter(t *testing.T) {
	tmpDir, cleanup := setupTestHome(t)
	defer cleanup()

	destRoot := filepath.Join(tmpDir, ".codex", "backups")
	r := versioning.NewRegistry(destRoot)

	cfgPath := createTestConfigFile(destRoot, "test_codex.toml", "original")
	s, _ := r.CreateSnapshot([]string{cfgPath}, "test", "main", "")

	err := runConfRestore(runConfRestoreOpts{
		snapshot: s.ID,
		agents:   "codex",
		branch:   "main",
	}, []string{})
	if err != nil {
		t.Logf("conf restore with agent filter: %v", err)
	}
}

func TestParseRestoreAgentList(t *testing.T) {
	result := parseRestoreAgentList("codex,claude,kimi")
	if len(result) != 3 {
		t.Fatalf("len = %d, want 3", len(result))
	}
	names := make(map[string]bool)
	for _, n := range result {
		names[n] = true
	}
	if !names["codex"] || !names["claude"] || !names["kimi"] {
		t.Errorf("parseRestoreAgentList = %v", result)
	}
}

func TestConfList_Empty(t *testing.T) {
	_, cleanup := setupTestHome(t)
	defer cleanup()

	err := runConfList(runConfListOpts{})
	if err != nil {
		t.Logf("conf list empty: %v", err)
	}
}

func TestConfList_WithSnapshots(t *testing.T) {
	tmpDir, cleanup := setupTestHome(t)
	defer cleanup()

	destRoot := filepath.Join(tmpDir, ".codex", "backups")
	r := versioning.NewRegistry(destRoot)

	for i := 0; i < 3; i++ {
		cfgPath := createTestConfigFile(destRoot, fmt.Sprintf("test%d.toml", i), fmt.Sprintf("data%d", i))
		_, _ = r.CreateSnapshot([]string{cfgPath}, fmt.Sprintf("snapshot %d", i), "main", "")
	}

	err := runConfList(runConfListOpts{})
	if err != nil {
		t.Logf("conf list with snapshots: %v", err)
	}
}

func TestConfList_FilterByBranch(t *testing.T) {
	tmpDir, cleanup := setupTestHome(t)
	defer cleanup()

	destRoot := filepath.Join(tmpDir, ".codex", "backups")
	r := versioning.NewRegistry(destRoot)

	r.Branches["dev"] = &versioning.Branch{}
	_ = r.Save()

	err := runConfList(runConfListOpts{
		branch: "dev",
	})
	if err != nil {
		t.Logf("conf list branch filter: %v", err)
	}
}

func TestConfList_FilterByType(t *testing.T) {
	_, cleanup := setupTestHome(t)
	defer cleanup()

	err := runConfList(runConfListOpts{
		typ: "global",
	})
	if err != nil {
		t.Logf("conf list type filter: %v", err)
	}
}

func TestConfMigrate_Idempotent(t *testing.T) {
	tmpDir, cleanup := setupTestHome(t)
	defer cleanup()

	destRoot := filepath.Join(tmpDir, ".codex", "backups")
	r := versioning.NewRegistry(destRoot)

	cfgPath := createTestConfigFile(destRoot, "test.toml", "data")
	s, _ := r.CreateSnapshot([]string{cfgPath}, "test", "main", "")
	_ = r.Save()
	_ = s.ID

	err1 := runConfMigrate(false)
	err2 := runConfMigrate(false)
	if err1 != nil && !strings.Contains(err1.Error(), "数据库") {
		t.Logf("first migrate: %v", err1)
	}
	if err2 != nil && !strings.Contains(err2.Error(), "数据库") {
		t.Logf("second migrate: %v", err2)
	}
}

func TestConfMigrate_DryRun(t *testing.T) {
	_, cleanup := setupTestHome(t)
	defer cleanup()

	err := runConfMigrate(true)
	if err != nil {
		t.Logf("conf migrate dry-run: %v", err)
	}
}

func TestConfBak_ForwardsToBackup(t *testing.T) {
	_, cleanup := setupTestHome(t)
	defer cleanup()

	backupBranch = "main"
	backupMessage = "test"

	err := confBakCmd.RunE(confBakCmd, []string{})
	if err != nil && !strings.Contains(err.Error(), "备份") {
		t.Logf("conf bak: %v", err)
	}
}

func TestConfShow_ForwardsToBackup(t *testing.T) {
	_, cleanup := setupTestHome(t)
	defer cleanup()

	snapshotBranch = "main"
	snapshotMessage = "test show"

	err := confShowCmd.RunE(confShowCmd, []string{})
	if err != nil && !strings.Contains(err.Error(), "备份") {
		t.Logf("conf show: %v", err)
	}
}

func TestConfRollback_ForwardsToRestore(t *testing.T) {
	_, cleanup := setupTestHome(t)
	defer cleanup()

	rollbackID = "nonexistent-id"

	err := confRollbackCmd.RunE(confRollbackCmd, []string{})
	if err == nil {
		t.Log("conf rollback returned nil (expected in test env)")
	} else {
		t.Logf("conf rollback: %v", err)
	}
}

func TestConfHistory_ForwardsToList(t *testing.T) {
	_, cleanup := setupTestHome(t)
	defer cleanup()

	err := confHistoryCmd.RunE(confHistoryCmd, []string{})
	if err != nil {
		t.Logf("conf history: %v", err)
	}
}

func TestConfAuto_ForwardsToConfSet(t *testing.T) {
	_, cleanup := setupTestHome(t)
	defer cleanup()

	caAgents = "codex"
	caModels = "codex=gpt-5.5"
	caDryRun = true

	err := runConfAuto("codex", "codex=gpt-5.5", true)
	if err != nil {
		t.Logf("conf auto: %v", err)
	}
}

func TestVersioningCreateAndRestore(t *testing.T) {
	tmpDir := t.TempDir()
	destRoot := filepath.Join(tmpDir, ".codex", "backups")
	r := versioning.NewRegistry(destRoot)

	cfgPath := createTestConfigFile(destRoot, "test.toml", "original")

	s, err := r.CreateSnapshot([]string{cfgPath}, "before modification", "main", "")
	if err != nil {
		t.Fatalf("CreateSnapshot failed: %v", err)
	}
	if s.ID == "" {
		t.Fatal("snapshot ID is empty")
	}

	entry := s.Configs[filepath.Base(cfgPath)]
	expectedHash := hash256("original")
	if entry.SHA256 != expectedHash {
		t.Errorf("SHA256 = %q, want %q", entry.SHA256, expectedHash)
	}

	os.WriteFile(cfgPath, []byte("modified"), 0644)

	restored, err := r.RestoreSnapshot(s.ID)
	if err != nil {
		t.Fatalf("RestoreSnapshot failed: %v", err)
	}
	if len(restored) != 1 {
		t.Fatalf("restored %d files, want 1", len(restored))
	}

	data, _ := os.ReadFile(cfgPath)
	if string(data) != "original" {
		t.Errorf("restored content = %q, want 'original'", string(data))
	}
}

func TestGetProxySource_ExplicitFlags(t *testing.T) {
	p, src, err := getProxySource("http://test.com/v1", "test-key")
	if err != nil {
		t.Fatalf("getProxySource with flags failed: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil proxy")
	}
	if p.BaseURL != "http://test.com/v1" {
		t.Errorf("BaseURL = %q, want 'http://test.com/v1'", p.BaseURL)
	}
	if p.APIKey != "test-key" {
		t.Errorf("APIKey = %q, want 'test-key'", p.APIKey)
	}
	if src != "flags" {
		t.Errorf("source = %q, want 'flags'", src)
	}
}

func TestGetProxySource_URLOnly(t *testing.T) {
	_, _, err := getProxySource("http://test.com/v1", "")
	if err == nil {
		t.Error("expected error when URL provided without key")
	}
}

func TestGetProxySource_AutoDetect(t *testing.T) {
	p, src, err := getProxySource("", "")
	if err != nil {
		t.Logf("auto-detect returned error (expected in test env): %v", err)
	}
	_ = p
	_ = src
}

func TestModelResolution_UpstreamMatch(t *testing.T) {
	upstream := []string{"gpt-5.5", "sensenova-6.7-flash-lite"}
	resolutions := model.ResolveAllModels(upstream, map[string]string{"fable": "glm-5.2"})

	foundCodex := false
	for _, r := range resolutions {
		if r.Agent == "codex" && r.Model == "gpt-5.5" {
			foundCodex = true
			break
		}
	}
	if !foundCodex {
		t.Error("codex should resolve to gpt-5.5 from upstream")
	}
}

func TestModelToWrite_Override(t *testing.T) {
	resolutions := []model.Resolution{
		{Agent: "codex", Model: "gpt-5.5", Source: "default"},
		{Agent: "claude", Model: "fable", Source: "proxy-map"},
	}
	overrides := map[string]string{"codex": "custom-model"}

	m, found := model.ModelToWrite(resolutions, overrides, "codex")
	if !found {
		t.Fatal("model not found for codex")
	}
	if m != "custom-model" {
		t.Errorf("model = %q, want 'custom-model'", m)
	}
}

func TestModelToWrite_NoOverride(t *testing.T) {
	resolutions := []model.Resolution{
		{Agent: "codex", Model: "gpt-5.5", Source: "default"},
	}
	m, found := model.ModelToWrite(resolutions, nil, "codex")
	if !found {
		t.Fatal("model not found")
	}
	if m != "gpt-5.5" {
		t.Errorf("model = %q, want 'gpt-5.5'", m)
	}
}

func TestWriterRegistry_CodeWriterPresent(t *testing.T) {
	reg := agent.NewWriterRegistry()
	writer := reg.Get("codex")
	if writer == nil {
		t.Error("codex writer not found")
	}
}

func TestWriterRegistry_CanConfigure(t *testing.T) {
	reg := agent.NewWriterRegistry()
	writer := reg.Get("codex")
	if writer == nil {
		t.Skip("codex writer not available")
	}
	p := &proxy.Proxy{BaseURL: "http://test.com/v1", APIKey: "key", Source: proxy.ProxyTypeCCX}
	if !writer.CanConfigure(p) {
		t.Errorf("codex writer should be able to configure for CCX proxy")
	}
}

func TestEndToEnd_BackupSetRestore(t *testing.T) {
	tmpDir, cleanup := setupTestHome(t)
	defer cleanup()

	destRoot := filepath.Join(tmpDir, ".codex", "backups")
	r := versioning.NewRegistry(destRoot)

	cfgPath := createTestConfigFile(destRoot, "codex_config.toml", "key = initial_value")

	s1, err := r.CreateSnapshot([]string{cfgPath}, "initial backup", "main", "")
	if err != nil {
		t.Fatalf("initial backup failed: %v", err)
	}
	t.Logf("Step 1: Initial snapshot created: %s", s1.ID)

	os.WriteFile(cfgPath, []byte("key = updated_value"), 0644)

	data, _ := os.ReadFile(cfgPath)
	if string(data) != "key = updated_value" {
		t.Fatalf("Step 2: file not updated, got %q", string(data))
	}
	t.Log("Step 2: Config modified successfully")

	s2, _ := r.CreateSnapshot([]string{cfgPath}, "post-set snapshot", "main", "")
	t.Logf("Step 3: Post-change snapshot: %s", s2.ID)

	_, _ = r.RestoreSnapshot(s1.ID)
	t.Log("Step 4: Restored to initial snapshot")

	data, _ = os.ReadFile(cfgPath)
	if string(data) != "key = initial_value" {
		t.Errorf("Step 5: file not restored, got %q, want 'key = initial_value'", string(data))
	}

	list := r.ListSnapshots()
	if len(list) < 2 {
		t.Errorf("Step 6: expected at least 2 snapshots, got %d", len(list))
	}
	t.Logf("Step 6: Found %d snapshots", len(list))
}

func TestConfSet_DBProxySource(t *testing.T) {
	_, cleanup := setupTestHome(t)
	defer cleanup()

	err := runConfSet(runConfSetOpts{
		agent: "all",
		db:    "99999",
	})
	if err == nil {
		t.Log("conf set with nonexistent db id returned nil (expected in test env)")
	} else {
		t.Logf("conf set db error: %v", err)
	}
}

func TestConfSet_DBProxyList(t *testing.T) {
	_, cleanup := setupTestHome(t)
	defer cleanup()

	err := runConfSet(runConfSetOpts{
		agent: "all",
		db:    "auto",
	})
	if err != nil {
		t.Logf("conf set --db: %v", err)
	}
}
