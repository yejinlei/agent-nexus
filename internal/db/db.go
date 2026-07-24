package db

import (
    "database/sql"
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "time"
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

type DB struct {
    db *sql.DB
}

func New() (*DB, error) {
    cwd, err := os.Getwd()
    if err != nil {
        return nil, fmt.Errorf("get working directory: %w", err)
    }
    dbPath := filepath.Join(cwd, "proxies.db")
    connStr := fmt.Sprintf("file:%s?mode=rwc", dbPath)
    sqlDB, err := sql.Open("sqlite", connStr)
    if err != nil {
        return nil, fmt.Errorf("open db: %w", err)
    }
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
    `)
    return err
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
