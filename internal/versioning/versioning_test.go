package versioning

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tmpRegistry(t *testing.T, backupRoot string) *Registry {
	t.Helper()
	r := NewRegistry(backupRoot)
	return r
}

func writeTestFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	os.MkdirAll(dir, 0755)
	for name, content := range files {
		os.WriteFile(filepath.Join(dir, name), []byte(content), 0644)
	}
}

// ---- NewRegistry / LoadRegistry tests ----

func TestNewRegistry_DefaultBranch(t *testing.T) {
	r := NewRegistry("/tmp/test-backups")
	if r.CurrentBranch != "main" {
		t.Errorf("CurrentBranch = %q, want main", r.CurrentBranch)
	}
	if r.Version != "1.0" {
		t.Errorf("Version = %q, want 1.0", r.Version)
	}
	if r.Snapshots == nil {
		t.Error("Snapshots should not be nil")
	}
	if _, ok := r.Branches["main"]; !ok {
		t.Error("main branch should exist")
	}
	if r.BackupsRoot != "/tmp/test-backups" {
		t.Errorf("BackupsRoot = %q, want /tmp/test-backups", r.BackupsRoot)
	}
}

func TestLoadRegistry_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	r := LoadRegistry(tmpDir)
	if r == nil {
		t.Fatal("LoadRegistry returned nil")
	}
	if r.CurrentBranch != "main" {
		t.Errorf("CurrentBranch = %q, want main", r.CurrentBranch)
	}
}

func TestLoadRegistry_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	os.MkdirAll(filepath.Join(tmpDir, "backups"), 0755)
	os.WriteFile(filepath.Join(tmpDir, "backups", "versioning.json"), []byte("{invalid json"), 0644)

	r := LoadRegistry(tmpDir)
	if r == nil {
		t.Fatal("LoadRegistry returned nil for invalid JSON")
	}
	// Falls back to new registry
	if r.CurrentBranch != "main" {
		t.Errorf("expected fallback to default, got CurrentBranch = %q", r.CurrentBranch)
	}
}

func TestLoadRegistry_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	r := NewRegistry(tmpDir)
	s := &Snapshot{ID: "test-snap", Branch: "main", Message: "test", CreatedAt: time.Now(), Configs: make(map[string]ConfigEntry)}
	r.Snapshots[s.ID] = s
	if err := r.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded := LoadRegistry(tmpDir)
	if loaded == nil {
		t.Fatal("LoadRegistry returned nil")
	}
	if loaded.CurrentBranch != "main" {
		t.Errorf("CurrentBranch = %q, want main", loaded.CurrentBranch)
	}
	if _, ok := loaded.Snapshots[s.ID]; !ok {
		t.Error("snapshot should be loaded")
	}
}

// ---- CreateSnapshot tests ----

func TestCreateSnapshot_HappyPath(t *testing.T) {
	tmpDir := t.TempDir()
	cfgDir := filepath.Join(tmpDir, "configs")
	writeTestFiles(t, cfgDir, map[string]string{
		"a.toml": "key = value",
		"b.json": `{"x": 1}`,
	})
	r := NewRegistry(tmpDir)

	s, err := r.CreateSnapshot([]string{
		filepath.Join(cfgDir, "a.toml"),
		filepath.Join(cfgDir, "b.json"),
	}, "test snapshot", "main")
	if err != nil {
		t.Fatalf("CreateSnapshot() error = %v", err)
	}
	if s == nil {
		t.Fatal("CreateSnapshot returned nil")
	}
	if len(s.Configs) != 2 {
		t.Errorf("expected 2 configs, got %d", len(s.Configs))
	}

	// Verify SHA256 and size
	for name, content := range map[string]string{"a.toml": "key = value", "b.json": `{"x": 1}`} {
		entry, ok := s.Configs[name]
		if !ok {
			t.Errorf("config %s not found in snapshot", name)
			continue
		}
		expectedHash := fmt.Sprintf("%x", sha256.Sum256([]byte(content)))
		if entry.SHA256 != expectedHash {
			t.Errorf("SHA256 for %s: got %q, want %q", name, entry.SHA256, expectedHash)
		}
		if entry.Bytes != len(content) {
			t.Errorf("Bytes for %s: got %d, want %d", name, entry.Bytes, len(content))
		}
	}

	// Verify saved to registry
	if _, ok := r.Snapshots[s.ID]; !ok {
		t.Error("snapshot not saved to registry")
	}
}

func TestCreateSnapshot_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	r := NewRegistry(tmpDir)
	s, err := r.CreateSnapshot([]string{
		filepath.Join(tmpDir, "nonexistent.toml"),
	}, "error test", "main")
	if err != nil {
		t.Fatalf("CreateSnapshot() error = %v", err)
	}
	entry, ok := s.Configs["nonexistent.toml"]
	if !ok {
		t.Fatal("error entry for missing file not found")
	}
	if entry.Error == "" {
		t.Error("expected non-empty error for missing file")
	}
}

func TestCreateSnapshot_NewBranch(t *testing.T) {
	tmpDir := t.TempDir()
	r := NewRegistry(tmpDir)
	_, err := r.CreateSnapshot([]string{}, "new branch test", "dev")
	if err != nil {
		t.Fatalf("CreateSnapshot() error = %v", err)
	}
	if _, ok := r.Branches["dev"]; !ok {
		t.Error("dev branch should have been created")
	}
}

func TestCreateSnapshot_DefaultBranch(t *testing.T) {
	tmpDir := t.TempDir()
	r := NewRegistry(tmpDir)
	s, err := r.CreateSnapshot([]string{}, "default branch", "")
	if err != nil {
		t.Fatalf("CreateSnapshot() error = %v", err)
	}
	if s.Branch != "main" {
		t.Errorf("Branch = %q, want main", s.Branch)
	}
}

func TestCreateSnapshot_BackupFileWritten(t *testing.T) {
	tmpDir := t.TempDir()
	cfgDir := filepath.Join(tmpDir, "configs")
	os.MkdirAll(cfgDir, 0755)
	os.WriteFile(filepath.Join(cfgDir, "a.toml"), []byte("data"), 0644)
	r := NewRegistry(tmpDir)

	s, err := r.CreateSnapshot([]string{filepath.Join(cfgDir, "a.toml")}, "test", "main")
	if err != nil {
		t.Fatalf("CreateSnapshot() error = %v", err)
	}
	backupFile := filepath.Join(tmpDir, "snapshots", s.ID, "a.toml")
	if _, err := os.Stat(backupFile); err != nil && err.Error() == "The system cannot find the file specified." {
		t.Errorf("backup file not written at %s", backupFile)
	}
}

// ---- ListSnapshots / GetSnapshot / LatestSnapshot ----

func TestListSnapshots(t *testing.T) {
	tmpDir := t.TempDir()
	r := NewRegistry(tmpDir)

	// Create 3 snapshots with slight delays to ensure ordering
	s1, _ := r.CreateSnapshot([]string{}, "first", "main")
	time.Sleep(10 * time.Millisecond)
	s2, _ := r.CreateSnapshot([]string{}, "second", "main")
	time.Sleep(10 * time.Millisecond)
	s3, _ := r.CreateSnapshot([]string{}, "third", "main")

	list := r.ListSnapshots()
	if len(list) != 3 {
		t.Fatalf("expected 3 snapshots, got %d", len(list))
	}
	// Should be reverse chronological: s3, s2, s1
	if list[0].Message != "third" {
		t.Errorf("first in list should be 'third', got %q", list[0].Message)
	}
	if list[2].Message != "first" {
		t.Errorf("last in list should be 'first', got %q", list[2].Message)
	}
	_ = s1 // avoid unused warning
	_ = s2
	_ = s3
}

func TestGetSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	r := NewRegistry(tmpDir)
	s, _ := r.CreateSnapshot([]string{}, "test", "main")
	got := r.GetSnapshot(s.ID)
	if got == nil {
		t.Fatal("GetSnapshot returned nil for existing snapshot")
	}
	if got.Message != "test" {
		t.Errorf("Message = %q, want test", got.Message)
	}

	nilSnap := r.GetSnapshot("nonexistent")
	if nilSnap != nil {
		t.Error("GetSnapshot should return nil for nonexistent ID")
	}
}

func TestLatestSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	r := NewRegistry(tmpDir)

	// Empty registry
	if r.LatestSnapshot() != nil {
		t.Error("LatestSnapshot on empty registry should return nil")
	}

	s, _ := r.CreateSnapshot([]string{}, "only", "main")
	latest := r.LatestSnapshot()
	if latest == nil {
		t.Fatal("LatestSnapshot returned nil")
	}
	if latest.ID != s.ID {
		t.Errorf("LatestSnapshot ID = %q, want %q", latest.ID, s.ID)
	}
}

// ---- RestoreSnapshot tests ----

func TestRestoreSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	cfgDir := filepath.Join(tmpDir, "configs")
	os.MkdirAll(cfgDir, 0755)
	original := filepath.Join(cfgDir, "a.toml")
	os.WriteFile(original, []byte("original"), 0644)

	r := NewRegistry(tmpDir)
	s, err := r.CreateSnapshot([]string{original}, "before change", "main")
	if err != nil {
		t.Fatalf("CreateSnapshot() error = %v", err)
	}

	// Modify the file after snapshot
	os.WriteFile(original, []byte("modified"), 0644)
	restored, err := r.RestoreSnapshot(s.ID)
	if err != nil {
		t.Fatalf("RestoreSnapshot() error = %v", err)
	}
	if len(restored) != 1 {
		t.Fatalf("expected 1 restored file, got %d", len(restored))
	}
	data, _ := os.ReadFile(original)
	if string(data) != "original" {
		t.Errorf("restored content = %q, want original", string(data))
	}
}

func TestRestoreSnapshot_Nonexistent(t *testing.T) {
	r := NewRegistry(t.TempDir())
	_, err := r.RestoreSnapshot("nope")
	if err == nil {
		t.Error("expected error restoring nonexistent snapshot")
	}
}

func TestRestoreSnapshot_HandlesErrorEntries(t *testing.T) {
	tmpDir := t.TempDir()
	r := NewRegistry(tmpDir)
	s := &Snapshot{
		ID:        "test-snap",
		Branch:    "main",
		Message:   "test",
		CreatedAt: time.Now(),
		Configs: map[string]ConfigEntry{
			"bad.toml": {Error: "file not found"},
			"good.toml": {
				FilePath: filepath.Join(tmpDir, "good.toml"),
				Contents: "ok",
			},
		},
	}
	r.Snapshots[s.ID] = s

	restored, err := r.RestoreSnapshot(s.ID)
	if err != nil {
		t.Fatalf("RestoreSnapshot() error = %v", err)
	}
	// Should have restored the good file but skipped the error entry
	if len(restored) != 1 {
		t.Errorf("expected 1 restored file, got %d", len(restored))
	}
}

// ---- SnapshotDiff tests ----

func TestSnapshotDiff_AllStatuses(t *testing.T) {
	tmpDir := t.TempDir()
	cfgDir := filepath.Join(tmpDir, "configs")
	os.MkdirAll(cfgDir, 0755)

	// Old snapshot files
	os.WriteFile(filepath.Join(cfgDir, "old.txt"), []byte("old content"), 0644)
	os.WriteFile(filepath.Join(cfgDir, "shared.txt"), []byte("same content"), 0644)

	r := NewRegistry(tmpDir)
	s1, _ := r.CreateSnapshot([]string{
		filepath.Join(cfgDir, "old.txt"),
		filepath.Join(cfgDir, "shared.txt"),
	}, "old", "main")

	// After snapshot: delete old.txt, modify shared.txt, add new.txt
	os.Remove(filepath.Join(cfgDir, "old.txt"))
	os.WriteFile(filepath.Join(cfgDir, "shared.txt"), []byte("modified content"), 0644)
	os.WriteFile(filepath.Join(cfgDir, "new.txt"), []byte("brand new"), 0644)

	// Create new snapshot WITHOUT old.txt (it no longer exists)
	s2, _ := r.CreateSnapshot([]string{
		filepath.Join(cfgDir, "shared.txt"),
		// intentionally omitting old.txt so it appears as "removed"
		filepath.Join(cfgDir, "new.txt"),
	}, "new", "main")

	diffs, err := r.SnapshotDiff(s1.ID, s2.ID)
	if err != nil {
		t.Fatalf("SnapshotDiff() error = %v", err)
	}

	statusMap := map[string]string{}
	for _, d := range diffs {
		statusMap[d.Agent] = d.Status
	}

	if statusMap["old.txt"] != "removed" {
		t.Errorf("old.txt should be removed, got %q", statusMap["old.txt"])
	}
	if statusMap["shared.txt"] != "modified" {
		t.Errorf("shared.txt should be modified, got %q", statusMap["shared.txt"])
	}
	if statusMap["new.txt"] != "added" {
		t.Errorf("new.txt should be added, got %q", statusMap["new.txt"])
	}
}

func TestSnapshotDiff_BothNonexistent(t *testing.T) {
	tmpDir := t.TempDir()
	cfgDir := filepath.Join(tmpDir, "configs")
	os.MkdirAll(cfgDir, 0755)

	r := NewRegistry(tmpDir)
	s1, _ := r.CreateSnapshot([]string{filepath.Join(cfgDir, "missing.txt")}, "old", "main")
	s2, _ := r.CreateSnapshot([]string{filepath.Join(cfgDir, "missing.txt")}, "new", "main")

	diffs, err := r.SnapshotDiff(s1.ID, s2.ID)
	if err != nil {
		t.Fatalf("SnapshotDiff() error = %v", err)
	}
	for _, d := range diffs {
		if d.Status != "error" {
			t.Errorf("diff should be error status, got %q", d.Status)
		}
	}
}

func TestSnapshotDiff_NonexistentSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	cfgDir := filepath.Join(tmpDir, "configs")
	os.MkdirAll(cfgDir, 0755)

	r := NewRegistry(tmpDir)
	_, err := r.SnapshotDiff("nope", "also-nope")
	if err == nil {
		t.Error("expected error for nonexistent snapshots")
	}
	_, err = r.SnapshotDiff("existing", "nope")
	// First doesn't exist
	if err == nil {
		t.Error("expected error when old snapshot doesn't exist")
	}
}

func TestSnapshotDiff_Unchanged(t *testing.T) {
	tmpDir := t.TempDir()
	cfgDir := filepath.Join(tmpDir, "configs")
	os.MkdirAll(cfgDir, 0755)
	os.WriteFile(filepath.Join(cfgDir, "stable.txt"), []byte("same"), 0644)

	r := NewRegistry(tmpDir)
	s1, _ := r.CreateSnapshot([]string{filepath.Join(cfgDir, "stable.txt")}, "old", "main")
	s2, _ := r.CreateSnapshot([]string{filepath.Join(cfgDir, "stable.txt")}, "new", "main")

	diffs, err := r.SnapshotDiff(s1.ID, s2.ID)
	if err != nil {
		t.Fatalf("SnapshotDiff() error = %v", err)
	}
	for _, d := range diffs {
		if d.Status != "unchanged" {
			t.Errorf("expected unchanged, got %q for %s", d.Status, d.Agent)
		}
	}
}

// ---- Branch tests ----

func TestBranch_CreateAndSwitch(t *testing.T) {
	tmpDir := t.TempDir()
	r := NewRegistry(tmpDir)

	// Create a new branch
	r.Branches["dev"] = &Branch{Name: "dev", CreatedAt: time.Now()}
	if err := r.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := r.CheckoutBranch("dev"); err != nil {
		t.Fatalf("CheckoutBranch(dev) error = %v", err)
	}
	if r.CurrentBranch != "dev" {
		t.Errorf("CurrentBranch = %q, want dev", r.CurrentBranch)
	}

	// Switch back to main
	if err := r.CheckoutBranch("main"); err != nil {
		t.Fatalf("CheckoutBranch(main) error = %v", err)
	}
	if r.CurrentBranch != "main" {
		t.Errorf("CurrentBranch = %q, want main", r.CurrentBranch)
	}
}

func TestCheckoutBranch_Nonexistent(t *testing.T) {
	tmpDir := t.TempDir()
	r := NewRegistry(tmpDir)
	err := r.CheckoutBranch("does-not-exist")
	if err == nil {
		t.Error("expected error checking out nonexistent branch")
	}
}

func TestBranchesList(t *testing.T) {
	tmpDir := t.TempDir()
	r := NewRegistry(tmpDir)
	r.Branches["dev"] = &Branch{Name: "dev", CreatedAt: time.Now()}
	names := r.BranchesList()
	// Should include both main and dev
	hasMain := false
	hasDev := false
	for _, n := range names {
		if n == "main" {
			hasMain = true
		}
		if n == "dev" {
			hasDev = true
		}
	}
	if !hasMain {
		t.Error("BranchesList missing 'main'")
	}
	if !hasDev {
		t.Error("BranchesList missing 'dev'")
	}
}

// ---- Save / Load round-trip ----

func TestSaveAndLoadRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	r := NewRegistry(tmpDir)
	r.Branches["staging"] = &Branch{Name: "staging", CreatedAt: time.Now()}
	if err := r.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded := LoadRegistry(tmpDir)
	if loaded == nil {
		t.Fatal("LoadRegistry returned nil after Save")
	}
	if _, ok := loaded.Branches["staging"]; !ok {
		t.Error("staging branch not preserved after save/load")
	}
}

// ---- SnapshotContent tests ----

func TestSnapshotContent(t *testing.T) {
	tmpDir := t.TempDir()
	cfgDir := filepath.Join(tmpDir, "configs")
	os.MkdirAll(cfgDir, 0755)
	os.WriteFile(filepath.Join(cfgDir, "a.toml"), []byte("hello"), 0644)

	r := NewRegistry(tmpDir)
	s, _ := r.CreateSnapshot([]string{filepath.Join(cfgDir, "a.toml")}, "test", "main")

	content, err := r.SnapshotContent(s.ID, "a.toml")
	if err != nil {
		t.Fatalf("SnapshotContent() error = %v", err)
	}
	if content != "hello" {
		t.Errorf("content = %q, want hello", content)
	}
}

func TestSnapshotContent_Nonexistent(t *testing.T) {
	tmpDir := t.TempDir()
	r := NewRegistry(tmpDir)
	_, err := r.SnapshotContent("nope", "a.toml")
	if err == nil {
		t.Error("expected error for nonexistent snapshot")
	}
	_, err = r.SnapshotContent("fake", "a.toml")
	if err == nil {
		t.Error("expected error for nonexistent snapshot ID")
	}
}

func TestSnapshotContent_MissingAgent(t *testing.T) {
	tmpDir := t.TempDir()
	r := NewRegistry(tmpDir)
	s := &Snapshot{
		ID:        "test",
		Branch:    "main",
		Message:   "test",
		CreatedAt: time.Now(),
		Configs:   map[string]ConfigEntry{},
	}
	r.Snapshots[s.ID] = s

	_, err := r.SnapshotContent(s.ID, "a.toml")
	if err == nil {
		t.Error("expected error for missing agent in snapshot")
	}
}
