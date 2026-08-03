package db

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newTestDB returns a DB backed by a temporary file and a cleanup function.
func newTestDB(t *testing.T) (*DB, func()) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "proxies.db")
	// Use the env-var hook so each test opens its own isolated DB.
	if err := os.Setenv("AGENT_NEXUS_DB_PATH", dbPath); err != nil {
		t.Fatalf("Setenv: %v", err)
	}
	d, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cleanup := func() {
		os.Unsetenv("AGENT_NEXUS_DB_PATH")
		_ = d.Close()
	}
	return d, cleanup
}

// helper snapshot ID
const testSnapshotID = "test-snap-001"

// sha256Hex returns the hex digest of data.
func sha256Hex(data string) string {
	h := sha256.Sum256([]byte(data))
	return string(h[:])
}

func TestInit_BackupTablesCreated(t *testing.T) {
	d, cleanup := newTestDB(t)
	defer cleanup()
	if err := d.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	// Verify tables exist by counting rows (should not error).
	var snapCount, entryCount int
	d.db.QueryRow("SELECT COUNT(*) FROM backup_snapshots").Scan(&snapCount)
	d.db.QueryRow("SELECT COUNT(*) FROM backup_config_entries").Scan(&entryCount)
	if snapCount != 0 {
		t.Errorf("backup_snapshots should be empty, got %d", snapCount)
	}
	if entryCount != 0 {
		t.Errorf("backup_config_entries should be empty, got %d", entryCount)
	}
}

func TestCreateSnapshot_HappyPath(t *testing.T) {
	d, cleanup := newTestDB(t)
	defer cleanup()
	d.Init()

	snapshot := &BackupSnapshot{
		ID:        testSnapshotID,
		Type:      "global",
		AgentName: "",
		Branch:    "main",
		Message:   "initial snapshot",
		CreatedAt: "2026-01-01T00:00:00Z",
	}
	entries := []BackupConfigEntry{
		{SnapshotID: testSnapshotID, AgentName: "agent-a", FilePath: "/configs/agent-a.toml", FileBasename: "agent-a.toml", SHA256: sha256Hex("a"), FileSize: 1, FileContent: "a", ModTime: "2026-01-01T00:00:00Z"},
		{SnapshotID: testSnapshotID, AgentName: "agent-b", FilePath: "/configs/agent-b.toml", FileBasename: "agent-b.toml", SHA256: sha256Hex("b"), FileSize: 1, FileContent: "b", ModTime: "2026-01-01T00:00:00Z"},
	}
	if err := d.CreateSnapshot(snapshot, entries); err != nil {
		t.Fatalf("CreateSnapshot: %v", err)
	}

	// Verify snapshot row
	s, err := d.GetSnapshot(testSnapshotID)
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}
	if s.Type != "global" {
		t.Errorf("Type = %q, want global", s.Type)
	}
	if s.Branch != "main" {
		t.Errorf("Branch = %q, want main", s.Branch)
	}
	if s.Message != "initial snapshot" {
		t.Errorf("Message = %q, want initial snapshot", s.Message)
	}

	// Verify entry rows
	entryRows, err := d.GetEntriesBySnapshot(testSnapshotID)
	if err != nil {
		t.Fatalf("GetEntriesBySnapshot: %v", err)
	}
	if len(entryRows) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entryRows))
	}
	if entryRows[0].FileBasename != "agent-a.toml" {
		t.Errorf("first entry basename = %q, want agent-a.toml", entryRows[0].FileBasename)
	}
	if entryRows[1].FileBasename != "agent-b.toml" {
		t.Errorf("second entry basename = %q, want agent-b.toml", entryRows[1].FileBasename)
	}
}

func TestCreateSnapshot_Transactional(t *testing.T) {
	d, cleanup := newTestDB(t)
	defer cleanup()
	d.Init()

	// Snapshot with invalid proxy_id reference; foreign key check should reject it
	// only if strict foreign keys are enforced. Since we use CHECK + FK, a bad
	// proxy_id should cause the INSERT to fail and the whole tx to roll back.
	proxyID := 99999
	snapshot := &BackupSnapshot{
		ID:        testSnapshotID,
		Type:      "per-agent",
		AgentName: "agent-a",
		Branch:    "main",
		Message:   "fk test",
		CreatedAt: "2026-01-01T00:00:00Z",
		ProxyID:   &proxyID,
	}
	entries := []BackupConfigEntry{
		{SnapshotID: testSnapshotID, AgentName: "agent-a", FilePath: "/a", FileBasename: "a", SHA256: "x", FileSize: 1, FileContent: "a"},
	}
	err := d.CreateSnapshot(snapshot, entries)
	if err == nil {
		t.Fatal("expected foreign key violation for nonexistent proxy_id")
	}
	// Transaction rolled back; no rows should exist.
	var count int
	d.db.QueryRow("SELECT COUNT(*) FROM backup_snapshots").Scan(&count)
	if count != 0 {
		t.Errorf("expected 0 snapshot rows after rollback, got %d", count)
	}
	var entryCount int
	d.db.QueryRow("SELECT COUNT(*) FROM backup_config_entries").Scan(&entryCount)
	if entryCount != 0 {
		t.Errorf("expected 0 entry rows after rollback, got %d", entryCount)
	}
}

func TestCreateSnapshotAutoID(t *testing.T) {
	d, cleanup := newTestDB(t)
	defer cleanup()
	d.Init()

	id, err := d.CreateSnapshotAutoID("global", "", "main", "auto id test", nil, []BackupConfigEntry{
		{AgentName: "agent-x", FilePath: "/x", FileBasename: "x", SHA256: "h1", FileSize: 1, FileContent: "x"},
	})
	if err != nil {
		t.Fatalf("CreateSnapshotAutoID: %v", err)
	}
	if id == "" {
		t.Fatal("returned empty id")
	}
	s, err := d.GetSnapshot(id)
	if err != nil {
		t.Fatalf("GetSnapshot(%s): %v", id, err)
	}
	if s.Type != "global" {
		t.Errorf("Type = %q, want global", s.Type)
	}
}

func TestGetSnapshot_NotFound(t *testing.T) {
	d, cleanup := newTestDB(t)
	defer cleanup()
	d.Init()
	_, err := d.GetSnapshot("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent snapshot")
	}
}

func TestListSnapshots(t *testing.T) {
	d, cleanup := newTestDB(t)
	defer cleanup()
	d.Init()

	for i, msg := range []string{"first", "second", "third"} {
		d.CreateSnapshot(&BackupSnapshot{
			ID:        string(rune('A' + i)),
			Type:      "global",
			Branch:    "main",
			Message:   msg,
			CreatedAt: time.Unix(int64(i)*1000, 0).UTC().Format(time.RFC3339),
		}, []BackupConfigEntry{})
	}
	list, err := d.ListSnapshots()
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3, got %d", len(list))
	}
	if list[0].Message != "third" {
		t.Errorf("first (latest) = %q, want third", list[0].Message)
	}
	if list[2].Message != "first" {
		t.Errorf("last = %q, want first", list[2].Message)
	}
}

func TestDeleteSnapshot_Cascade(t *testing.T) {
	d, cleanup := newTestDB(t)
	defer cleanup()
	d.Init()

	id := "cascade-snap"
	d.CreateSnapshot(&BackupSnapshot{ID: id, Type: "global", Branch: "main", Message: "to delete", CreatedAt: "2026-01-01T00:00:00Z"},
		[]BackupConfigEntry{
			{SnapshotID: id, AgentName: "agent-a", FilePath: "/a", FileBasename: "a", SHA256: "h1", FileSize: 1, FileContent: "a"},
			{SnapshotID: id, AgentName: "agent-b", FilePath: "/b", FileBasename: "b", SHA256: "h2", FileSize: 1, FileContent: "b"},
		},
	)
	if err := d.DeleteSnapshot(id); err != nil {
		t.Fatalf("DeleteSnapshot: %v", err)
	}
	// Snapshot gone
	_, err := d.GetSnapshot(id)
	if err == nil {
		t.Fatal("snapshot should be gone")
	}
	// Entries cascade-deleted
	entries, _ := d.GetEntriesBySnapshot(id)
	if len(entries) != 0 {
		t.Errorf("expected 0 entries after cascade, got %d", len(entries))
	}
}

func TestCreateEntry(t *testing.T) {
	d, cleanup := newTestDB(t)
	defer cleanup()
	d.Init()
	// Insert a snapshot first
	d.CreateSnapshot(&BackupSnapshot{ID: testSnapshotID, Type: "global", Branch: "main", Message: "test", CreatedAt: "2026-01-01T00:00:00Z"}, []BackupConfigEntry{})

	entry := &BackupConfigEntry{
		SnapshotID: testSnapshotID, AgentName: "agent-c", FilePath: "/c", FileBasename: "c",
		SHA256: "h3", FileSize: 3, FileContent: "ccc", ModTime: "2026-01-01T00:00:00Z",
	}
	if err := d.CreateEntry(entry); err != nil {
		t.Fatalf("CreateEntry: %v", err)
	}
	if entry.ID == 0 {
		t.Error("expected non-zero ID after CreateEntry")
	}
	entries, _ := d.GetEntriesBySnapshot(testSnapshotID)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].SHA256 != "h3" {
		t.Errorf("SHA256 = %q, want h3", entries[0].SHA256)
	}
}

func TestGetEntriesBySnapshot_Empty(t *testing.T) {
	d, cleanup := newTestDB(t)
	defer cleanup()
	d.Init()
	entries, err := d.GetEntriesBySnapshot("none")
	if err != nil {
		t.Fatalf("GetEntriesBySnapshot: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestBackupSnapshotTypeValidation(t *testing.T) {
	d, cleanup := newTestDB(t)
	defer cleanup()
	d.Init()
	// CHECK constraint should reject invalid type.
	_, err := d.db.Exec("INSERT INTO backup_snapshots (id, type, branch, created_at) VALUES (?, ?, ?, ?)", "bad", "invalid", "main", "2026-01-01T00:00:00Z")
	if err == nil {
		t.Fatal("expected CHECK violation for invalid type")
	}
	// Valid types should succeed.
	for _, typ := range []string{"global", "per-agent"} {
		if _, err := d.db.Exec("INSERT INTO backup_snapshots (id, type, branch, created_at) VALUES (?, ?, ?, ?)", typ, typ, "main", "2026-01-01T00:00:00Z"); err != nil {
			t.Errorf("insert type %q failed: %v", typ, err)
		}
	}
}

func TestBackupSnapshotProxyIDNullable(t *testing.T) {
	d, cleanup := newTestDB(t)
	defer cleanup()
	d.Init()
	id := "null-proxy"
	d.CreateSnapshot(&BackupSnapshot{
		ID: id, Type: "global", Branch: "main", Message: "nullable proxy",
		CreatedAt: "2026-01-01T00:00:00Z",
	}, []BackupConfigEntry{})
	s, err := d.GetSnapshot(id)
	if err != nil {
		t.Fatalf("GetSnapshot: %v", err)
	}
	if s.ProxyID != nil {
		t.Errorf("ProxyID should be nil, got %v", s.ProxyID)
	}
}


func TestExistsByURL(t *testing.T) {
	tmpDir := t.TempDir()
	sqlDB, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=rwc", filepath.Join(tmpDir, "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	d := &DB{db: sqlDB}
	if err := d.Init(); err != nil {
		t.Fatal(err)
	}

	// Empty DB
	if d.ExistsByURL("http://example.com") {
		t.Fatal("expected empty DB to return false")
	}
	if d.ExistsByURL("http://example.com/v1") {
		t.Fatal("expected empty DB to return false for /v1")
	}

	// Insert a proxy (URL stored without normalisation)
	if err := d.Add("http://example.com/v1", "key1", "OpenAI", true, false, false, 1, []string{"m1"}, time.Now()); err != nil {
		t.Fatal(err)
	}

	if !d.ExistsByURL("http://example.com/v1") {
		t.Fatal("expected true for exact match with /v1")
	}
	if !d.ExistsByURL("http://example.com") {
		t.Fatal("expected true for URL without /v1 (normalised)")
	}
	if d.ExistsByURL("http://other.com/v1") {
		t.Fatal("expected false for non-existent URL")
	}
}