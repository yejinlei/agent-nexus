package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

type ProxyRecord struct {
	ID             int       `db:"id"`
	URL            string    `db:"url"`
	Key            string    `db:"key"`
	DetectedFormat string    `db:"detected_format"`
	OpenAICap      bool      `db:"openai_cap"`
	AnthropicCap   bool      `db:"anthropic_cap"`
	ModelCount     int       `db:"model_count"`
	ModelsJSON     string    `db:"models_json"`
	CreatedAt      time.Time `db:"created_at"`
}

// BackupSnapshot represents a point-in-time backup of agent configs.
type BackupSnapshot struct {
	ID        string `db:"id"`
	Type      string `db:"type"`
	AgentName string `db:"agent_name"`
	Branch    string `db:"branch"`
	Message   string `db:"message"`
	CreatedAt string `db:"created_at"`
	ProxyID   *int   `db:"proxy_id"`
}

// BackupConfigEntry represents a single config file within a snapshot.
type BackupConfigEntry struct {
	ID           int    `db:"id"`
	SnapshotID   string `db:"snapshot_id"`
	AgentName    string `db:"agent_name"`
	FilePath     string `db:"file_path"`
	FileBasename string `db:"file_basename"`
	SHA256       string `db:"sha256"`
	FileSize     int    `db:"file_size"`
	FileContent  string `db:"file_content"`
	ModTime      string `db:"mod_time"`
	Error        string `db:"error"`
}

// ProxyModelRecord represents one upstream model from a proxy.
// For custom-model agents, these model IDs are passed directly to the agent.
// For redirect agents, a matching ProxyModelMapping row links a native model to an upstream model.
type ProxyModelRecord struct {
	ID        int    `db:"id"`
	ProxyID   int    `db:"proxy_id"`
	ModelID   string `db:"model_id"`
	Category  string `db:"category"`
	CreatedAt string `db:"created_at"`
}

// ProxyModelMapping represents a redirect mapping: native → upstream model.
// Used when a redirect agent (kimi, hermes, qoder, trae) is configured
// against a proxy from a given DB record.
type ProxyModelMapping struct {
	ID            int    `db:"id"`
	ProxyID       int    `db:"proxy_id"`
	AgentName     string `db:"agent_name"`
	NativeModel   string `db:"native_model"`
	UpstreamModel string `db:"upstream_model"`
	Reason        string `db:"reason"`
	CreatedAt     string `db:"created_at"`
}

type DB struct {
	db *sql.DB
}

func New() (*DB, error) {
	// Override hook for tests / portable setups: env wins when set.
	if env := os.Getenv("AGENT_NEXUS_DB_PATH"); env != "" {
		return newDB(env)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("get user home: %w", err)
	}
	dbPath := filepath.Join(home, ".agent-nexus", "proxies.db")
	return newDB(dbPath)
}

func newDB(dbPath string) (*DB, error) {
	dir := filepath.Dir(dbPath)
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create db directory: %w", err)
		}
	}
	connStr := fmt.Sprintf("file:%s?mode=rwc", dbPath)
	sqlDB, err := sql.Open("sqlite", connStr)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// Enable foreign keys (PRAGMA is not effective via URL param in modernc.org/sqlite).
	_, _ = sqlDB.Exec("PRAGMA foreign_keys = ON")
	return &DB{db: sqlDB}, nil
}
func (d *DB) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

func (d *DB) Init() error {
	_, err := d.db.Exec(`
		CREATE TABLE IF NOT EXISTS proxies (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			url            TEXT NOT NULL,
			key            TEXT NOT NULL,
			detected_format TEXT,
			openai_cap     INTEGER NOT NULL DEFAULT 0,
			anthropic_cap  INTEGER NOT NULL DEFAULT 0,
			model_count    INTEGER NOT NULL DEFAULT 0,
			models_json    TEXT,
			created_at     TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS backup_snapshots (
			id             TEXT PRIMARY KEY,
			type           TEXT NOT NULL CHECK (type IN ('global', 'per-agent')),
			agent_name     TEXT,
			branch         TEXT NOT NULL DEFAULT 'main',
			message        TEXT,
			created_at     TEXT NOT NULL,
			proxy_id       INTEGER,
			FOREIGN KEY (proxy_id) REFERENCES proxies(id)
		);
		CREATE INDEX IF NOT EXISTS idx_snapshots_branch_time ON backup_snapshots(branch, created_at DESC);
		CREATE INDEX IF NOT EXISTS idx_snapshots_type_agent  ON backup_snapshots(type, agent_name);
		CREATE TABLE IF NOT EXISTS backup_config_entries (
			id             INTEGER PRIMARY KEY AUTOINCREMENT,
			snapshot_id    TEXT NOT NULL REFERENCES backup_snapshots(id) ON DELETE CASCADE,
			agent_name     TEXT NOT NULL,
			file_path      TEXT NOT NULL,
			file_basename  TEXT NOT NULL,
			sha256         TEXT NOT NULL,
			file_size      INTEGER NOT NULL,
			file_content   TEXT NOT NULL,
			mod_time       TEXT,
			error          TEXT
		);
		CREATE INDEX IF NOT EXISTS idx_entries_snapshot ON backup_config_entries(snapshot_id);
		CREATE INDEX IF NOT EXISTS idx_entries_agent    ON backup_config_entries(agent_name);
		CREATE INDEX IF NOT EXISTS idx_entries_sha256   ON backup_config_entries(sha256);
	CREATE TABLE IF NOT EXISTS proxy_model_mappings (
		id             INTEGER PRIMARY KEY AUTOINCREMENT,
		proxy_id       INTEGER NOT NULL REFERENCES proxies(id) ON DELETE CASCADE,
		agent_name     TEXT    NOT NULL,
		native_model   TEXT    NOT NULL,
		upstream_model TEXT    NOT NULL,
		reason         TEXT    NOT NULL DEFAULT 'keyword',
		created_at     TEXT    NOT NULL,
		UNIQUE (proxy_id, agent_name, native_model)
	);
	CREATE INDEX IF NOT EXISTS idx_model_mapping_proxy ON proxy_model_mappings(proxy_id);
	CREATE INDEX IF NOT EXISTS idx_model_mapping_agent ON proxy_model_mappings(agent_name);
	`)
	return err
}

// CreateSnapshot inserts a snapshot and its config entries in a single transaction.
func (d *DB) CreateSnapshot(snapshot *BackupSnapshot, entries []BackupConfigEntry) error {
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.Exec(`
		INSERT INTO backup_snapshots (id, type, agent_name, branch, message, created_at, proxy_id)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, snapshot.ID, snapshot.Type, snapshot.AgentName, snapshot.Branch, snapshot.Message, snapshot.CreatedAt, snapshot.ProxyID)
	if err != nil {
		return fmt.Errorf("insert snapshot: %w", err)
	}

	for _, e := range entries {
		_, err = tx.Exec(`
			INSERT INTO backup_config_entries (snapshot_id, agent_name, file_path, file_basename, sha256, file_size, file_content, mod_time, error)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, e.SnapshotID, e.AgentName, e.FilePath, e.FileBasename, e.SHA256, e.FileSize, e.FileContent, e.ModTime, e.Error)
		if err != nil {
			return fmt.Errorf("insert entry %s: %w", e.FileBasename, err)
		}
	}

	return tx.Commit()
}

// CreateSnapshotAutoID generates a UUID snapshot ID and creates the snapshot+entries atomically.
func (d *DB) CreateSnapshotAutoID(snapshotType, agentName, branch, message string, proxyID *int, entries []BackupConfigEntry) (string, error) {
	snapshot := &BackupSnapshot{
		ID:        uuid.New().String(),
		Type:      snapshotType,
		AgentName: agentName,
		Branch:    branch,
		Message:   message,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		ProxyID:   proxyID,
	}
	for i := range entries {
		entries[i].SnapshotID = snapshot.ID
	}
	if err := d.CreateSnapshot(snapshot, entries); err != nil {
		return "", err
	}
	return snapshot.ID, nil
}

func (d *DB) GetSnapshot(id string) (*BackupSnapshot, error) {
	var s BackupSnapshot
	var proxyID sql.NullInt64
	err := d.db.QueryRow(`
		SELECT id, type, agent_name, branch, message, created_at, proxy_id
		FROM backup_snapshots
		WHERE id = ?
	`, id).Scan(&s.ID, &s.Type, &s.AgentName, &s.Branch, &s.Message, &s.CreatedAt, &proxyID)
	if err != nil {
		return nil, err
	}
	if proxyID.Valid {
		i := int(proxyID.Int64)
		s.ProxyID = &i
	}
	return &s, nil
}

// ListSnapshots returns all snapshots ordered by created_at descending.
func (d *DB) ListSnapshots() ([]BackupSnapshot, error) {
	rows, err := d.db.Query(`
		SELECT id, type, agent_name, branch, message, created_at, proxy_id
		FROM backup_snapshots
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []BackupSnapshot
	for rows.Next() {
		var s BackupSnapshot
		var proxyID sql.NullInt64
		if err := rows.Scan(&s.ID, &s.Type, &s.AgentName, &s.Branch, &s.Message, &s.CreatedAt, &proxyID); err != nil {
			return nil, err
		}
		if proxyID.Valid {
			i := int(proxyID.Int64)
			s.ProxyID = &i
		}
		list = append(list, s)
	}
	return list, rows.Err()
}

// DeleteSnapshot removes a snapshot; cascade deletes the config entries automatically.
func (d *DB) DeleteSnapshot(id string) error {
	_, err := d.db.Exec("DELETE FROM backup_snapshots WHERE id = ?", id)
	return err
}

func (d *DB) CreateEntry(entry *BackupConfigEntry) error {
	result, err := d.db.Exec(`
		INSERT INTO backup_config_entries (snapshot_id, agent_name, file_path, file_basename, sha256, file_size, file_content, mod_time, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, entry.SnapshotID, entry.AgentName, entry.FilePath, entry.FileBasename, entry.SHA256, entry.FileSize, entry.FileContent, entry.ModTime, entry.Error)
	if err != nil {
		return fmt.Errorf("insert config entry: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("get last insert id: %w", err)
	}
	entry.ID = int(id)
	return nil
}

// GetEntriesBySnapshot returns all config entries for a given snapshot.
func (d *DB) GetEntriesBySnapshot(snapshotID string) ([]BackupConfigEntry, error) {
	rows, err := d.db.Query(`
		SELECT id, snapshot_id, agent_name, file_path, file_basename, sha256, file_size, file_content, mod_time, error
		FROM backup_config_entries
		WHERE snapshot_id = ?
		ORDER BY id
	`, snapshotID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []BackupConfigEntry
	for rows.Next() {
		var e BackupConfigEntry
		var modTime, fileContent, errStr sql.NullString
		if err := rows.Scan(&e.ID, &e.SnapshotID, &e.AgentName, &e.FilePath, &e.FileBasename, &e.SHA256, &e.FileSize, &fileContent, &modTime, &errStr); err != nil {
			return nil, err
		}
		if fileContent.Valid {
			e.FileContent = fileContent.String
		}
		if modTime.Valid {
			e.ModTime = modTime.String
		}
		if errStr.Valid {
			e.Error = errStr.String
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

func (d *DB) Add(url, key, detectedFormat string, openaiCap, anthropicCap bool, modelCount int, modelIDs []string, createdAt time.Time) error {
	modelsJSON, _ := json.Marshal(modelIDs)
	var maxID sql.NullInt64
	err := d.db.QueryRow("SELECT MAX(id) FROM proxies").Scan(&maxID)
	if err != nil {
		return fmt.Errorf("query max id: %w", err)
	}
	nextID := 1
	if maxID.Valid {
		nextID = int(maxID.Int64) + 1
	}
	_, err = d.db.Exec(`
		INSERT INTO proxies (id, url, key, detected_format, openai_cap, anthropic_cap, model_count, models_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);
	`, nextID, url, key, detectedFormat, boolToInt(openaiCap), boolToInt(anthropicCap), modelCount, string(modelsJSON), createdAt.Format(time.RFC3339))
	return err
}

// ExistsByURL checks whether a proxy record with the given URL already exists.
// Normalises the URL the same way the sniff module does: strips trailing slash
// and appends "/v1" if absent, so the comparison matches what sniff.Sniff returns.
func (d *DB) ExistsByURL(url string) bool {
	url = strings.TrimSuffix(url, "/")
	if !strings.HasSuffix(url, "/v1") {
		url += "/v1"
	}
	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM proxies WHERE url = ?", url).Scan(&count)
	return err == nil && count > 0
}
func (d *DB) List() ([]ProxyRecord, error) {
	rows, err := d.db.Query(`
		SELECT id, url, key, detected_format, openai_cap, anthropic_cap, model_count, models_json, created_at
		FROM proxies
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []ProxyRecord
	for rows.Next() {
		var r ProxyRecord
		var oaiCap, antCap int64
		var ts string
		if err := rows.Scan(&r.ID, &r.URL, &r.Key, &r.DetectedFormat, &oaiCap, &antCap, &r.ModelCount, &r.ModelsJSON, &ts); err != nil {
			return nil, err
		}
		r.OpenAICap = oaiCap != 0
		r.AnthropicCap = antCap != 0
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			r.CreatedAt = t
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

func (d *DB) GetByID(id int) (*ProxyRecord, error) {
	var r ProxyRecord
	var oaiCap, antCap int64
	var ts string
	err := d.db.QueryRow(`
		SELECT id, url, key, detected_format, openai_cap, anthropic_cap, model_count, models_json, created_at
		FROM proxies
		WHERE id = ?
	`, id).Scan(&r.ID, &r.URL, &r.Key, &r.DetectedFormat, &oaiCap, &antCap, &r.ModelCount, &r.ModelsJSON, &ts)
	if err != nil {
		return nil, err
	}
	r.OpenAICap = oaiCap != 0
	r.AnthropicCap = antCap != 0
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		r.CreatedAt = t
	}
	return &r, nil
}

func (d *DB) Delete(id int) error {
	_, err := d.db.Exec("DELETE FROM proxies WHERE id = ?", id)
	return err
}

func (d *DB) Truncate() error {
	if err := d.Init(); err != nil {
		return err
	}
	_, err := d.db.Exec("DELETE FROM proxies")
	return err
}

func (d *DB) TruncateReset() error {
	if err := d.Init(); err != nil {
		return err
	}
	_, err := d.db.Exec("DELETE FROM proxies")
	if err != nil {
		return err
	}
	_, err = d.db.Exec("DELETE FROM sqlite_sequence WHERE name = 'proxies'")
	return err
}

// GetMinIDProxy returns the proxy record with the smallest (earliest) id.
// Returns nil when the table is empty or no valid record exists.
func (d *DB) GetMinIDProxy() (*ProxyRecord, error) {
	var r ProxyRecord
	var oaiCap, antCap int64
	var ts string
	err := d.db.QueryRow(`
		SELECT id, url, key, detected_format, openai_cap, anthropic_cap, model_count, models_json, created_at
		FROM proxies
		ORDER BY id ASC
		LIMIT 1
	`).Scan(&r.ID, &r.URL, &r.Key, &r.DetectedFormat, &oaiCap, &antCap, &r.ModelCount, &r.ModelsJSON, &ts)
	if err != nil {
		return nil, err
	}
	r.OpenAICap = oaiCap != 0
	r.AnthropicCap = antCap != 0
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		r.CreatedAt = t
	}
	return &r, nil
}

// UpsertProxyModelMapping upserts a single redirect mapping for a proxy.
func (d *DB) UpsertProxyModelMapping(pm *ProxyModelMapping) error {
	_, err := d.db.Exec(`
		INSERT INTO proxy_model_mappings (proxy_id, agent_name, native_model, upstream_model, reason, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT DO UPDATE SET
			upstream_model = excluded.upstream_model,
			reason = excluded.reason
	`, pm.ProxyID, pm.AgentName, pm.NativeModel, pm.UpstreamModel, pm.Reason, pm.CreatedAt)
	return err
}

// GetModelsFromRecord parses the models_json field of a ProxyRecord into a
// deduplicated, sorted model ID list. Returns nil when no models are stored.
func GetModelsFromRecord(r *ProxyRecord) []string {
	if r == nil || r.ModelsJSON == "" {
		return nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(r.ModelsJSON), &ids); err != nil {
		return nil
	}
	// Deduplicate while preserving order, then sort for stable output.
	seen := make(map[string]bool)
	var deduped []string
	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			deduped = append(deduped, id)
		}
	}
	return deduped
}

// GetModelMappingsByAgent returns all mappings for a given agent+proxy.
// Returns nil slice (not nil) when no mappings exist.
func (d *DB) GetModelMappingsByAgent(proxyID int, agentName string) ([]ProxyModelMapping, error) {
	rows, err := d.db.Query(`
		SELECT id, proxy_id, agent_name, native_model, upstream_model, reason, created_at
		FROM proxy_model_mappings
		WHERE proxy_id = ? AND agent_name = ?
		ORDER BY id
	`, proxyID, agentName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []ProxyModelMapping
	for rows.Next() {
		var m ProxyModelMapping
		if err := rows.Scan(&m.ID, &m.ProxyID, &m.AgentName, &m.NativeModel, &m.UpstreamModel, &m.Reason, &m.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, m)
	}
	return list, rows.Err()
}

// GetModelMappingByNative returns a single redirect mapping row.
func (d *DB) GetModelMappingByNative(proxyID int, agentName, nativeModel string) (*ProxyModelMapping, error) {
	var m ProxyModelMapping
	err := d.db.QueryRow(`
		SELECT id, proxy_id, agent_name, native_model, upstream_model, reason, created_at
		FROM proxy_model_mappings
		WHERE proxy_id = ? AND agent_name = ? AND native_model = ?
	`, proxyID, agentName, nativeModel).Scan(&m.ID, &m.ProxyID, &m.AgentName, &m.NativeModel, &m.UpstreamModel, &m.Reason, &m.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// DeleteModelMappingsByProxy removes all redirect mappings for a proxy.
func (d *DB) DeleteModelMappingsByProxy(proxyID int) error {
	_, err := d.db.Exec("DELETE FROM proxy_model_mappings WHERE proxy_id = ?", proxyID)
	return err
}

// GetAllModelMappingsByProxy returns all redirect mappings for a proxy
// across all agents. Returns nil slice (not nil) when no mappings exist.
func (d *DB) GetAllModelMappingsByProxy(proxyID int) []ProxyModelMapping {
	rows, err := d.db.Query(`
		SELECT id, proxy_id, agent_name, native_model, upstream_model, reason, created_at
		FROM proxy_model_mappings
		WHERE proxy_id = ?
		ORDER BY id
	`, proxyID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var list []ProxyModelMapping
	for rows.Next() {
		var m ProxyModelMapping
		if err := rows.Scan(&m.ID, &m.ProxyID, &m.AgentName, &m.NativeModel, &m.UpstreamModel, &m.Reason, &m.CreatedAt); err != nil {
			continue
		}
		list = append(list, m)
	}
	if list == nil {
		list = []ProxyModelMapping{}
	}
	return list
}

func (d *DB) Count() (int, error) {
	var count int
	err := d.db.QueryRow("SELECT COUNT(*) FROM proxies").Scan(&count)
	return count, err
}

func (d *DB) Query(filter string) ([]ProxyRecord, error) {
	if filter == "" {
		return d.List()
	}
	var idNum int
	for _, c := range filter {
		if c >= '0' && c <= '9' {
			idNum = idNum*10 + int(c-'0')
		} else {
			idNum = -1
			break
		}
	}
	if idNum > 0 {
		record, getErr := d.GetByID(idNum)
		if getErr != nil {
			return nil, getErr
		}
		if record == nil {
			return nil, nil
		}
		return []ProxyRecord{*record}, nil
	}
	rows, err := d.db.Query(`
		SELECT id, url, key, detected_format, openai_cap, anthropic_cap, model_count, models_json, created_at
		FROM proxies
		WHERE url LIKE ?
		ORDER BY created_at DESC
	`, "%"+filter+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var records []ProxyRecord
	for rows.Next() {
		var r ProxyRecord
		var oaiCap, antCap int64
		var ts string
		if err := rows.Scan(&r.ID, &r.URL, &r.Key, &r.DetectedFormat, &oaiCap, &antCap, &r.ModelCount, &r.ModelsJSON, &ts); err != nil {
			return nil, err
		}
		r.OpenAICap = oaiCap != 0
		r.AnthropicCap = antCap != 0
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			r.CreatedAt = t
		}
		records = append(records, r)
	}
	return records, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
