package versioning

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Snapshot represents a point-in-time capture of all agent config files
type Snapshot struct {
	ID        string            `json:"id"`        // format: YYYY-MM-DD_HH-MM-SS
	Branch    string            `json:"branch"`    // branch name (default: "main")
	Message   string            `json:"message"`   // commit message
	CreatedAt time.Time         `json:"created_at"`
	Configs   map[string]ConfigEntry `json:"configs"` // agent name -> config data
}

// ConfigEntry represents a single config file in a snapshot
type ConfigEntry struct {
	FilePath   string            `json:"file_path"`   // original file path on disk
	Contents   string            `json:"contents"`    // raw file content
	SHA256     string            `json:"sha256"`      // SHA-256 hash of raw content
	Bytes      int               `json:"bytes"`       // raw file size
	ModifiedAt time.Time         `json:"modified_at"` // original file mod time
	Error      string            `json:"error,omitempty"` // error if read failed
}

// Branch represents a named branch of configuration versions
type Branch struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// Registry is the top-level versioning metadata store
type Registry struct {
	Version      string           `json:"version"`
	BackupsRoot  string           `json:"backups_root"`
	Snapshots    map[string]*Snapshot `json:"snapshots"`  // id -> snapshot
	Branches     map[string]*Branch   `json:"branches"`   // name -> branch
	CurrentBranch string            `json:"current_branch"` // default: "main"
}

// RegistryPath returns the default path to the versioning.json metadata file
func RegistryPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex", "backups", "versioning.json")
}

// NewRegistry creates a fresh registry for the given backup root directory
func NewRegistry(backupsRoot string) *Registry {
	return &Registry{
		Version:      "1.0",
		BackupsRoot:  backupsRoot,
		Snapshots:    make(map[string]*Snapshot),
		Branches:     map[string]*Branch{"main": {Name: "main", CreatedAt: time.Now()}},
		CurrentBranch: "main",
	}
}

// LoadRegistry reads the registry from disk; returns an empty registry on error
func LoadRegistry(backupsRoot string) *Registry {
	r := NewRegistry(backupsRoot)
	path := filepath.Join(backupsRoot, "versioning.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return r
	}
	if err := json.Unmarshal(data, r); err != nil {
		return NewRegistry(backupsRoot)
	}
	if r.Snapshots == nil {
		r.Snapshots = make(map[string]*Snapshot)
	}
	if r.Branches == nil {
		r.Branches = map[string]*Branch{"main": {Name: "main", CreatedAt: time.Now()}}
	}
	if r.CurrentBranch == "" {
		r.CurrentBranch = "main"
	}
	return r
}

// SaveRegistry writes the registry to disk
func (r *Registry) Save() error {
	dir := filepath.Dir(filepath.Join(r.BackupsRoot, "versioning.json"))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.BackupsRoot, "versioning.json"), data, 0644)
}

// CreateSnapshot captures the current state of the specified config files
func (r *Registry) CreateSnapshot(configPaths []string, message string, branch string) (*Snapshot, error) {
	if branch == "" {
		branch = r.CurrentBranch
	}
	// Ensure the branch exists
	if _, ok := r.Branches[branch]; !ok {
		r.Branches[branch] = &Branch{Name: branch, CreatedAt: time.Now()}
	}

	snapshot := &Snapshot{
		ID:        time.Now().Format("2006-01-02_15-04-05.000000"),
		Branch:    branch,
		Message:   message,
		CreatedAt: time.Now(),
		Configs:   make(map[string]ConfigEntry),
	}

	for _, path := range configPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			// Store error entry; snapshot is still valid for other files
			snapshot.Configs[filepath.Base(path)] = ConfigEntry{
				FilePath: path,
				Error:    err.Error(),
			}
			continue
		}

		hash := sha256.Sum256(data)
		info, _ := os.Stat(path)

		snapshot.Configs[filepath.Base(path)] = ConfigEntry{
			FilePath:   path,
			Contents:   string(data),
			SHA256:     fmt.Sprintf("%x", hash),
			Bytes:      len(data),
			ModifiedAt: info.ModTime(),
		}
	}

	// Also save the raw backup copy
	if err := r.saveSnapshotBackup(snapshot); err != nil {
		// Log the error but continue; metadata is saved
		fmt.Fprintf(os.Stderr, "warning: failed to save snapshot backup files: %v\n", err)
	}

	r.Snapshots[snapshot.ID] = snapshot
	return snapshot, r.Save()
}

// saveSnapshotBackup writes the raw backup files for a snapshot
func (r *Registry) saveSnapshotBackup(s *Snapshot) error {
	destDir := filepath.Join(r.BackupsRoot, "snapshots", s.ID)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return err
	}
	for name, entry := range s.Configs {
		if entry.Error != "" {
			continue
		}
		dst := filepath.Join(destDir, name)
		if err := os.WriteFile(dst, []byte(entry.Contents), 0644); err != nil {
			return err
		}
	}
	return nil
}

// ListSnapshots returns all snapshots in reverse chronological order
func (r *Registry) ListSnapshots() []*Snapshot {
	var list []*Snapshot
	for _, s := range r.Snapshots {
		list = append(list, s)
	}
	// Sort by created time descending
	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			if list[j].CreatedAt.After(list[i].CreatedAt) {
				list[i], list[j] = list[j], list[i]
			}
		}
	}
	return list
}

// GetSnapshot returns a snapshot by ID, or nil if not found
func (r *Registry) GetSnapshot(id string) *Snapshot {
	return r.Snapshots[id]
}

// LatestSnapshot returns the most recent snapshot, or nil if none exist
func (r *Registry) LatestSnapshot() *Snapshot {
	snapshots := r.ListSnapshots()
	if len(snapshots) == 0 {
		return nil
	}
	return snapshots[0]
}

// RestoreSnapshot restores all config files from a snapshot to their original locations
func (r *Registry) RestoreSnapshot(id string) ([]string, error) {
	s, ok := r.Snapshots[id]
	if !ok {
		return nil, fmt.Errorf("snapshot %s not found", id)
	}

	var restored []string
	for name, entry := range s.Configs {
		if entry.Error != "" {
			fmt.Printf("  ⚠ %s: was not captured (%s)\n", name, entry.Error)
			continue
		}

		// Create parent directory if needed
		dir := filepath.Dir(entry.FilePath)
		if err := os.MkdirAll(dir, 0755); err != nil {
			fmt.Printf("  ❌ %s: failed to create directory %s: %v\n", name, dir, err)
			continue
		}

		if err := os.WriteFile(entry.FilePath, []byte(entry.Contents), 0644); err != nil {
			fmt.Printf("  ❌ %s: write failed: %v\n", name, err)
			continue
		}

		restored = append(restored, entry.FilePath)
		fmt.Printf("  ✅ %s → %s\n", name, entry.FilePath)
	}
	return restored, nil
}

// SnapshotDiff compares two snapshots and returns files that changed
func (r *Registry) SnapshotDiff(oldID, newID string) ([]FileDiff, error) {
	old, ok := r.Snapshots[oldID]
	if !ok {
		return nil, fmt.Errorf("old snapshot %s not found", oldID)
	}
	new, ok := r.Snapshots[newID]
	if !ok {
		return nil, fmt.Errorf("new snapshot %s not found", newID)
	}

	var diffs []FileDiff

	// Collect all agent names from both snapshots
	agentSet := make(map[string]bool)
	for name := range old.Configs {
		agentSet[name] = true
	}
	for name := range new.Configs {
		agentSet[name] = true
	}

	for name := range agentSet {
		oldEntry, oldExists := old.Configs[name]
		newEntry, newExists := new.Configs[name]

		// Handle errors
		if oldEntry.Error != "" || newEntry.Error != "" {
			diffs = append(diffs, FileDiff{
				Agent:       name,
				Status:      "error",
				Message:     fmt.Sprintf("old:%s new:%s", oldEntry.Error, newEntry.Error),
			})
			continue
		}

		// File only in old snapshot (removed)
		if !newExists && oldExists {
			diffs = append(diffs, FileDiff{
				Agent:  name,
				Status: "removed",
				OldSize: oldEntry.Bytes,
			})
			continue
		}

		// File only in new snapshot (added)
		if newExists && !oldExists {
			diffs = append(diffs, FileDiff{
				Agent:   name,
				Status:  "added",
				NewSize: newEntry.Bytes,
			})
			continue
		}

		// Both exist — check if changed
		if oldEntry.SHA256 != newEntry.SHA256 {
			diffs = append(diffs, FileDiff{
				Agent:    name,
				Status:   "modified",
				OldSHA256: oldEntry.SHA256,
				NewSHA256: newEntry.SHA256,
				OldSize:  oldEntry.Bytes,
				NewSize:  newEntry.Bytes,
			})
		} else {
			diffs = append(diffs, FileDiff{
                Agent:   name,
                Status:  "unchanged",
            })
        }
	}

	return diffs, nil
}

// CheckoutBranch switches the current branch
func (r *Registry) CheckoutBranch(name string) error {
	if _, ok := r.Branches[name]; !ok {
		return fmt.Errorf("branch %s does not exist", name)
	}
	r.CurrentBranch = name
	return r.Save()
}

// SnapshotContent retrieves the raw content of a config file from a snapshot
func (r *Registry) SnapshotContent(id string, agentName string) (string, error) {
	s, ok := r.Snapshots[id]
	if !ok {
		return "", fmt.Errorf("snapshot %s not found", id)
	}
	entry, ok := s.Configs[agentName]
	if !ok {
		return "", fmt.Errorf("agent %s not found in snapshot %s", agentName, id)
	}
	if entry.Error != "" {
		return "", fmt.Errorf("%s: %s", agentName, entry.Error)
	}
	return entry.Contents, nil
}

// FileDiff represents the diff of one config file between two snapshots
type FileDiff struct {
	Agent       string `json:"agent"`
	Status      string `json:"status"`  // added / modified / removed / unchanged / error
	Message     string `json:"message,omitempty"`
	OldSHA256   string `json:"old_sha256,omitempty"`
	NewSHA256   string `json:"new_sha256,omitempty"`
	OldSize     int    `json:"old_size,omitempty"`
	NewSize     int    `json:"new_size,omitempty"`
}

// BranchesList returns all branch names
func (r *Registry) BranchesList() []string {
	var names []string
	for name := range r.Branches {
		names = append(names, name)
	}
	return names
}
