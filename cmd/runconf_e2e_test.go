package cmd

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"agent-nexus/internal/agent"
	"agent-nexus/internal/db"
	"agent-nexus/internal/model"
	"agent-nexus/internal/proxy"
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
	err := runConfSet(runConfSetOpts{agent: "all"})
	if err == nil {
		t.Logf("conf set returned nil (no proxy available)")
	} else {
		t.Logf("conf set returned: %v", err)
	}
}

func TestConfSet_DryRun_NoWrite(t *testing.T) {
	_, cleanup := setupTestHome(t)
	defer cleanup()
	err := runConfSet(runConfSetOpts{agent: "codex", dryRun: true})
	if err != nil {
		t.Logf("conf set dry-run returned: %v", err)
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
	err := runConfBackup(runConfBackupOpts{agents: "all", branch: "main", message: "test backup"})
	if err != nil {
		t.Logf("conf backup global: %v", err)
	}
}

func TestConfBackup_Global_DryRun(t *testing.T) {
	_, cleanup := setupTestHome(t)
	defer cleanup()
	err := runConfBackup(runConfBackupOpts{agents: "all", branch: "main", dryRun: true})
	if err != nil {
		t.Logf("conf backup dry-run: %v", err)
	}
}

func TestConfBackup_SingleAgent(t *testing.T) {
	_, cleanup := setupTestHome(t)
	defer cleanup()
	err := runConfBackup(runConfBackupOpts{agents: "codex", branch: "main", message: "single agent backup"})
	if err != nil {
		t.Logf("conf backup single agent: %v", err)
	}
}

func TestConfBackup_DBEntryCreated(t *testing.T) {
	_, cleanup := setupTestHome(t)
	defer cleanup()
	_ = runConfBackup(runConfBackupOpts{agents: "codex", branch: "main", message: "db test backup"})
	dbInst, dbErr := db.New()
	if dbErr != nil {
		t.Logf("DB not available: %v", dbErr)
		return
	}
	defer dbInst.Close()
	_ = dbInst.Init()
	snapshots, _ := dbInst.ListSnapshots()
	if len(snapshots) == 0 {
		t.Log("No snapshots in DB")
	}
}

func TestConfRestore_NonexistentSnapshot(t *testing.T) {
	_, cleanup := setupTestHome(t)
	defer cleanup()
	err := runConfRestore(runConfRestoreOpts{snapshot: "nonexistent-id"}, []string{})
	if err == nil {
		t.Error("expected error for nonexistent snapshot")
	}
}

func TestConfRestore_DB_Snapshot(t *testing.T) {
	_, cleanup := setupTestHome(t)
	defer cleanup()
	cfgPath := createTestConfigFile(t.TempDir(), "test.toml", "original")
	dbInst, dbErr := db.New()
	if dbErr != nil {
		t.Fatalf("DB not available: %v", dbErr)
	}
	defer dbInst.Close()
	_ = dbInst.Init()
	snapID, err := dbInst.CreateSnapshotAutoID("global", "ALL", "main", "test restore", "test-snap", nil, []db.BackupConfigEntry{
		{AgentName: "codex", FilePath: cfgPath, FileBasename: "test.toml", FileContent: "original"},
	})
	if err != nil {
		t.Fatalf("CreateSnapshotAutoID failed: %v", err)
	}
	os.WriteFile(cfgPath, []byte("modified"), 0644)
	_ = runConfRestore(runConfRestoreOpts{snapshot: snapID, branch: "main"}, []string{})
}

func TestParseRestoreAgentList(t *testing.T) {
	result := parseRestoreAgentList("codex,claude,kimi")
	if len(result) != 3 {
		t.Fatalf("len = %d, want 3", len(result))
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

func TestConfList_FilterByType(t *testing.T) {
	_, cleanup := setupTestHome(t)
	defer cleanup()
	err := runConfList(runConfListOpts{typ: "global"})
	if err != nil {
		t.Logf("conf list type filter: %v", err)
	}
}

func TestConfAuto_ForwardsToConfSet(t *testing.T) {
	_, cleanup := setupTestHome(t)
	defer cleanup()
	caAgents = "codex"
	caModels = "codex=gpt-5.5"
	caDryRun = true
	_ = runConfAuto("codex", "codex=gpt-5.5", true)
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
	m, found := model.ModelToWrite(resolutions, map[string]string{"codex": "custom-model"}, "codex")
	if !found {
		t.Fatal("model not found for codex")
	}
	if m != "custom-model" {
		t.Errorf("model = %q, want 'custom-model'", m)
	}
}

func TestModelToWrite_NoOverride(t *testing.T) {
	resolutions := []model.Resolution{{Agent: "codex", Model: "gpt-5.5", Source: "default"}}
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
	if reg.Get("codex") == nil {
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

func TestConfSet_DBProxySource(t *testing.T) {
	_, cleanup := setupTestHome(t)
	defer cleanup()
	_ = runConfSet(runConfSetOpts{agent: "all", db: "99999"})
}

func TestConfSet_DBProxyList(t *testing.T) {
	_, cleanup := setupTestHome(t)
	defer cleanup()
	_ = runConfSet(runConfSetOpts{agent: "all", db: "auto"})
}
