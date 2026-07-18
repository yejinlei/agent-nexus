package backup

import (
	"os"
	"path/filepath"
	"time"
)

type BackupResult struct {
	Source  string
	Dest    string
	Success bool
	Error   string
}

func Backup(configPaths []string, destRoot string) ([]BackupResult, error) {
	ts := time.Now().Format("2006-01-02_15-04-05")
	destDir := filepath.Join(destRoot, "backups", "agent-configs-"+ts)

	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, err
	}

	results := []BackupResult{}
	for _, src := range configPaths {
		basename := filepath.Base(src)
		dst := filepath.Join(destDir, basename)

		data, err := os.ReadFile(src)
		if err != nil {
			results = append(results, BackupResult{
				Source: src, Dest: dst, Success: false,
				Error: "read failed: " + err.Error(),
			})
			continue
		}

		if err := os.WriteFile(dst, data, 0644); err != nil {
			results = append(results, BackupResult{
				Source: src, Dest: dst, Success: false,
				Error: "write failed: " + err.Error(),
			})
			continue
		}

		results = append(results, BackupResult{
			Source: src, Dest: dst, Success: true,
		})
	}

	return results, nil
}

func LatestBackupDir(destRoot string) string {
	entries, _ := os.ReadDir(filepath.Join(destRoot, "backups"))
	var latest string
	var latestTime time.Time

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, _ := e.Info()
		if info.ModTime().After(latestTime) {
			latestTime = info.ModTime()
			latest = e.Name()
		}
	}
	if latest == "" {
		return ""
	}
	return filepath.Join(destRoot, "backups", latest)
}
