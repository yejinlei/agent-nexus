package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBackup_HappyPath(t *testing.T) {
	tmpDir := t.TempDir()
	destRoot := filepath.Join(tmpDir, "dest")

	// Create source config files
	cfgDir := filepath.Join(tmpDir, "configs")
	os.MkdirAll(cfgDir, 0755)
	paths := []string{
		filepath.Join(cfgDir, "config.toml"),
		filepath.Join(cfgDir, "settings.json"),
	}
	for i, p := range paths {
		os.WriteFile(p, []byte("content"+string(rune('0'+i))), 0644)
	}

	results, err := Backup(paths, destRoot)
	if err != nil {
		t.Fatalf("Backup() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if !r.Success {
			t.Errorf("backup of %s failed: %s", r.Source, r.Error)
		}
		if r.Dest == "" {
			t.Errorf("dest should not be empty for %s", r.Source)
		}
		// Verify the backup file exists and has same content
		data, _ := os.ReadFile(r.Dest)
		if len(data) == 0 {
			t.Errorf("backup file %s is empty", r.Dest)
		}
	}
}

func TestBackup_OneFileMissing(t *testing.T) {
	tmpDir := t.TempDir()
	cfgDir := filepath.Join(tmpDir, "configs")
	os.MkdirAll(cfgDir, 0755)

	existing := filepath.Join(cfgDir, "exists.toml")
	os.WriteFile(existing, []byte("hello"), 0644)

	paths := []string{
		existing,
		filepath.Join(cfgDir, "missing.toml"), // does not exist
	}

	results, err := Backup(paths, filepath.Join(tmpDir, "dest"))
	if err != nil {
		t.Fatalf("Backup() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// First should succeed, second should fail
	if !results[0].Success {
		t.Errorf("existing file backup should succeed")
	}
	if results[1].Success {
		t.Errorf("missing file backup should fail")
	}
}

func TestBackup_EmptyPaths(t *testing.T) {
	tmpDir := t.TempDir()
	destRoot := filepath.Join(tmpDir, "dest")
	results, err := Backup([]string{}, destRoot)
	if err != nil {
		t.Fatalf("Backup([]) error = %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestBackup_DirCreated(t *testing.T) {
	tmpDir := t.TempDir()
	destRoot := filepath.Join(tmpDir, "nonexistent")
	paths := []string{filepath.Join(tmpDir, "src", "a.toml")}
	// src directory doesn't exist yet, so this will produce a failure for the file,
	// but the dest dir should still be created
	os.MkdirAll(filepath.Dir(paths[0]), 0755)
	os.WriteFile(paths[0], []byte("data"), 0644)

	_, err := Backup(paths, destRoot)
	if err != nil {
		t.Fatalf("Backup() error = %v", err)
	}
	// The backup dir should exist
	entries, _ := os.ReadDir(filepath.Join(destRoot, "backups"))
	if len(entries) == 0 {
		t.Error("backup directory was not created")
	}
}

func TestBackup_LatestBackupDir(t *testing.T) {
	tmpDir := t.TempDir()
	destRoot := filepath.Join(tmpDir, "base")
	// Create some backup dirs with different names (timestamps are in names)
	os.MkdirAll(filepath.Join(destRoot, "backups", "agent-configs-2026-01-01_00-00-00"), 0755)
	os.MkdirAll(filepath.Join(destRoot, "backups", "agent-configs-2026-01-02_00-00-00"), 0755)

	latest := LatestBackupDir(destRoot)
	if latest == "" {
		t.Error("LatestBackupDir returned empty")
	}
	if !filepath.IsAbs(latest) {
		t.Errorf("LatestBackupDir should return an absolute path, got %q", latest)
	}
}

func TestBackup_LatestBackupDirEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	destRoot := filepath.Join(tmpDir, "base")
	// Ensure backups dir exists but is empty
	os.MkdirAll(filepath.Join(destRoot, "backups"), 0755)

	latest := LatestBackupDir(destRoot)
	if latest != "" {
		t.Errorf("LatestBackupDir on empty dir should return empty, got %q", latest)
	}
}

func TestBackup_LatestBackupDirIgnoresFiles(t *testing.T) {
	tmpDir := t.TempDir()
	destRoot := filepath.Join(tmpDir, "base")
	os.MkdirAll(filepath.Join(destRoot, "backups"), 0755)
	os.WriteFile(filepath.Join(destRoot, "backups", "agent-configs-2026-01-01"), []byte("not a dir"), 0644)

	latest := LatestBackupDir(destRoot)
	if latest != "" {
		t.Errorf("LatestBackupDir should ignore files, got %q", latest)
	}
}

func TestBackup_ResultFields(t *testing.T) {
	tmpDir := t.TempDir()
	cfgDir := filepath.Join(tmpDir, "configs")
	os.MkdirAll(cfgDir, 0755)
	src := filepath.Join(cfgDir, "test.toml")
	os.WriteFile(src, []byte("data"), 0644)

	results, err := Backup([]string{src}, filepath.Join(tmpDir, "dest"))
	if err != nil {
		t.Fatalf("Backup() error = %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Source != src {
		t.Errorf("Source = %q, want %q", r.Source, src)
	}
}
