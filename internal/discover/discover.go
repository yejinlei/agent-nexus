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
		// === 可配置 CLI Agent（通过代理转发） ===
		{Name: "codex", Category: "cli", ConfigFiles: []string{"Codex/config.toml"}, IsConfigurable: true},
		{Name: "claude", Category: "cli", ConfigFiles: []string{"Claude/settings.json"}, IsConfigurable: true},
		{Name: "kimi", Category: "cli", ConfigFiles: []string{".kimi/config.toml"}, HomeDirFiles: []string{".kimi/config.toml"}, IsConfigurable: true},
		{Name: "deepseek", Category: "cli", HomeDirFiles: []string{".deepseek/config.toml"}, IsConfigurable: true},
		{Name: "opencode", Category: "cli", ConfigFiles: []string{".config/opencode/opencode.jsonc"}, IsConfigurable: true},
		{Name: "openclaw", Category: "cli", HomeDirFiles: []string{".openclaw/openclaw.json"}, IsConfigurable: true},
		{Name: "cursor", Category: "ide", ConfigFiles: []string{"Cursor/User/settings.json"}, IsConfigurable: true},
		// CodeBuddy CLI（腾讯，Claude Code 兼容）
		{Name: "codebuddy", Category: "cli", HomeDirFiles: []string{".codebuddy/settings.json"}, IsConfigurable: true},
		// Hermes CLI（Nous Research，ACP 协议）
		{Name: "hermes", Category: "cli", HomeDirFiles: []string{".hermes/config.yaml"}, IsConfigurable: true},
		// Kiro CLI（Amazon，ACP 协议）
		{Name: "kiro", Category: "cli", HomeDirFiles: []string{".kiro/config.yaml"}, IsConfigurable: true},
		// Grok CLI（xAI，ACP 协议）
		{Name: "grok", Category: "cli", HomeDirFiles: []string{".grok/config.yaml"}, IsConfigurable: true},
		// Qoder CLI（阿里，ACP 协议）
		{Name: "qoder", Category: "cli", HomeDirFiles: []string{".qoder/config.yaml"}, IsConfigurable: true},
		// Trae CLI（字节跳动，ACP 协议）
		{Name: "trae", Category: "cli", HomeDirFiles: []string{".traecli/config.yaml"}, IsConfigurable: true},

		// === 不可配置 CLI Agent（无外部模型配置字段） ===
		// Antigravity（Google，Gemini 后端，无外部配置）
		{Name: "antigravity", Category: "cli", HomeDirFiles: []string{".agents/config.yaml"}, IsConfigurable: false, Notes: "使用 Google Gemini 服务，无外部模型配置字段"},
		// Copilot（GitHub，账户权益决定模型）
		{Name: "copilot", Category: "cli", ConfigFiles: []string{".config/github-copilot/config.yaml"}, IsConfigurable: false, Notes: "模型由 GitHub 账户权益决定，无外部模型配置字段"},
		// DevEco Code（华为，OpenCode 引擎，自有模型目录）
		{Name: "deveco", Category: "cli", ConfigFiles: []string{".config/deveco/deveco.jsonc"}, IsConfigurable: false, Notes: "基于 OpenCode 引擎，内置华为账号认证与自有模型目录"},
		// Pi（Inflection AI，无外部配置）
		{Name: "pi", Category: "cli", HomeDirFiles: []string{".pi/config.yaml"}, IsConfigurable: false, Notes: "Inflection AI 代理，无外部模型配置字段"},

		// === 不可配置 IDE（自有 AI 后端） ===
		// Qoder IDE（VS Code 派生，自有 AI 后端）
		{Name: "qoder-ide", Category: "ide", ConfigFiles: []string{"Qoder/User/settings.json"}, IsConfigurable: false, Notes: "使用自有AI后端，无外部模型配置字段"},
		// Trae IDE（VS Code 派生，自有 AI 后端）
		{Name: "trae-ide", Category: "ide", ConfigFiles: []string{"Trae/User/settings.json"}, IsConfigurable: false, Notes: "使用自有AI后端，无外部模型配置字段"},
		// CodeBuddy IDE（VS Code 派生，自有 AI 后端）
		{Name: "codebuddy-ide", Category: "ide", ConfigFiles: []string{"CodeBuddy/User/settings.json"}, IsConfigurable: false, Notes: "使用自有AI后端，无外部模型配置字段"},
		// Windsurf（VS Code 派生，自有 AI 后端）
		{Name: "windsurf", Category: "ide", ConfigFiles: []string{"Windsurf/User/settings.json"}, IsConfigurable: false, Notes: "使用自有AI后端，无外部模型配置字段"},
		// Zed（无内置 AI Agent）
		{Name: "zed", Category: "ide", ConfigFiles: []string{"Zed/settings.json"}, IsConfigurable: false, Notes: "无内置AI Agent，依赖外部工具"},

		// === 其他（暂缺配置写入器） ===
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
					// Skip IDE variants that already have CLI versions found
					if strings.HasSuffix(ap.Name, "-ide") {
						continue
					}
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
