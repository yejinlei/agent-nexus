package discover

import (
	"os"
	"path/filepath"
	"strings"
)

// AgentInfo represents a discovered AI coding agent
type AgentInfo struct {
	Name           string
	Category       string // "cli" or "ide"
	HasConfig      bool
	ConfigPath     string
	IsConfigured   bool
	IsConfigurable bool
	Notes          string
}

// AgentRegistry holds the known agent paths to check
type AgentRegistry struct {
	agents []AgentPath
}

type AgentPath struct {
	Name           string
	Category       string
	ConfigFiles    []string // paths relative to %APPDATA%\Roaming
	HomeDirFiles   []string // paths relative to $HOME (e.g. .codex/)
	IsConfigurable bool     // has external model provider config field
	Notes          string
}

var registry = AgentRegistry{
	agents: []AgentPath{
		{Name: "codex", Category: "cli", ConfigFiles: []string{"Codex/config.toml"}, IsConfigurable: true},
		{Name: "claude", Category: "cli", ConfigFiles: []string{"Claude/settings.json"}, IsConfigurable: true},
		{Name: "kimi", Category: "cli", ConfigFiles: []string{".kimi/config.toml"}, HomeDirFiles: []string{".kimi/config.toml"}, IsConfigurable: true},
		{Name: "deepseek", Category: "cli", HomeDirFiles: []string{".deepseek/config.toml"}, IsConfigurable: true},
		{Name: "opencode", Category: "cli", ConfigFiles: []string{".config/opencode/opencode.jsonc"}, IsConfigurable: true},
		{Name: "openclaw", Category: "cli", HomeDirFiles: []string{".openclaw/openclaw.json"}, IsConfigurable: true},
		{Name: "cursor", Category: "ide", ConfigFiles: []string{"Cursor/User/settings.json"}, IsConfigurable: true},
		{Name: "qoder", Category: "ide", ConfigFiles: []string{"Qoder/User/settings.json"}, IsConfigurable: false, Notes: "使用自有AI后端，无外部模型配置字段"},
		{Name: "trae", Category: "ide", ConfigFiles: []string{"Trae/User/settings.json"}, IsConfigurable: false, Notes: "使用自有AI后端，无外部模型配置字段"},
		{Name: "codebuddy", Category: "ide", ConfigFiles: []string{"CodeBuddy/User/settings.json"}, IsConfigurable: false, Notes: "使用自有AI后端，无外部模型配置字段"},
		{Name: "windsurf", Category: "ide", ConfigFiles: []string{"Windsurf/User/settings.json"}, IsConfigurable: false, Notes: "使用自有AI后端，无外部模型配置字段"},
		{Name: "zed", Category: "ide", ConfigFiles: []string{"Zed/settings.json"}, IsConfigurable: false, Notes: "无内置AI Agent，依赖外部工具"},
		{Name: "lmstudio", Category: "cli", ConfigFiles: []string{"LM Studio/settings.json"}, IsConfigurable: true},
		{Name: "clawx", Category: "ide", HomeDirFiles: []string{"AppData/Roaming/clawx/clawx-providers.json"}, IsConfigurable: true},
	},
}

// Discover scans for all known agents and returns their info
func Discover() []AgentInfo {
	home, _ := os.UserHomeDir()
	roaming := filepath.Join(home, "AppData", "Roaming")
	results := []AgentInfo{}

	for _, ap := range registry.agents {
		var configPath string
		var found bool

		for _, rel := range ap.HomeDirFiles {
			p := filepath.Join(home, rel)
			if _, err := os.Stat(p); err == nil {
				configPath = p
				found = true
				break
			}
		}

		if !found {
			for _, rel := range ap.ConfigFiles {
				p := filepath.Join(roaming, rel)
				if _, err := os.Stat(p); err == nil {
					configPath = p
					found = true
					break
				}
			}
		}

		info := AgentInfo{
			Name:           ap.Name,
			Category:       ap.Category,
			HasConfig:      found,
			ConfigPath:     configPath,
			IsConfigurable: ap.IsConfigurable,
			Notes:          ap.Notes,
		}

		if found && ap.IsConfigurable {
			info.IsConfigured = checkConfigured(configPath)
		}

		results = append(results, info)
	}

	return results
}

func checkConfigured(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	content := strings.ToLower(string(data))
	return strings.Contains(content, "127.0.0.1") &&
		(strings.Contains(content, "3688") || strings.Contains(content, "sensenova"))
}

// GetRegistry returns the known agent registry
func GetRegistry() []AgentPath {
	return registry.agents
}
