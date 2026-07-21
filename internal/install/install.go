package install

import (
	"fmt"
	"runtime"
	"strings"
)

// Platform constants
const (
	PlatformWindows = "windows"
	PlatformDarwin  = "darwin"
	PlatformLinux   = "linux"
)

// Agent represents an installable AI agent runtime
type Agent struct {
	Name       string            // machine name
	Display    string            // human-readable name
	Category   string            // "cli" or "ide"
	NpmPackage     string            // npm package name (empty if not on npm)
	UninstallPaths []string         // relative home paths to remove on uninstall (e.g. ".kimi-code")
	LegacyBinPaths  []string         // individual files to remove on uninstall (e.g. ".local/bin/kimi-legacy.exe")
	PipPackage string            // pip package name (empty if not via pip)
	Download   map[string]string // platform -> download URL
	Protocol   string            // "openai" or "acp"
	Notes      string            // additional notes
}

// registry is the list of installable agent runtimes
var registry = []Agent{
	{
		Name:      "codex",
		Display:   "Codex (CLI)",
		Category:  "cli",
		NpmPackage: "@openai/codex",
		UninstallPaths: []string{".codex"},
		Download:  map[string]string{},
		Protocol:  "openai",
		Notes:     "OpenAI Codex CLI — openai-compatible provider",
	},
	{
		Name:      "claude",
		Display:   "Claude Code",
		Category:  "cli",
		NpmPackage: "@anthropic-ai/claude-code",
		UninstallPaths: []string{".claude"},
		Download:  map[string]string{},
		Protocol:  "openai",
		Notes:     "Anthropic Claude Code — npm install, env-based proxy config",
	},
	{
		Name:      "kimi",
		Display:   "Kimi Code CLI",
		Category:  "cli",
		NpmPackage: "",
		UninstallPaths: []string{".kimi-code", ".kimi"},
		LegacyBinPaths: []string{".local/bin/kimi-legacy.exe", ".local/bin/kimi-cli.exe"},
		Download:  map[string]string{
			PlatformWindows: "https://code.kimi.com/kimi-code/install.ps1",
			PlatformDarwin:  "https://code.kimi.com/kimi-code/install.sh",
			PlatformLinux:   "https://code.kimi.com/kimi-code/install.sh",
		},
		Protocol: "acp",
		Notes:    "Kimi Code CLI — official installer; config at ~/.kimi-code/config.toml",
	},
	{
		Name:      "opencode",
		Display:   "OpenCode",
		Category:  "cli",
		NpmPackage: "",
		UninstallPaths: []string{".config/opencode"},
		Download:  map[string]string{
			PlatformWindows: "https://github.com/opencode-ai/opencode/releases/latest/download/opencode-windows-amd64.exe",
			PlatformDarwin:  "https://github.com/opencode-ai/opencode/releases/latest/download/opencode-darwin-arm64",
			PlatformLinux:   "https://github.com/opencode-ai/opencode/releases/latest/download/opencode-linux-amd64",
		},
		Protocol:  "openai",
		Notes:     "OpenCode CLI — JSON config with provider map",
	},
	{
		Name:      "openclaw",
		Display:   "OpenClaw",
		Category:  "cli",
		NpmPackage: "",
		UninstallPaths: []string{".openclaw"},
		Download:  map[string]string{
			PlatformWindows: "https://github.com/openclaw/openclaw/releases/latest/download/openclaw-windows-amd64.exe",
			PlatformDarwin:  "https://github.com/openclaw/openclaw/releases/latest/download/openclaw-darwin-arm64",
			PlatformLinux:   "https://github.com/openclaw/openclaw/releases/latest/download/openclaw-linux-amd64",
		},
		Protocol:  "openai",
		Notes:     "OpenClaw CLI — JSON config with model providers",
	},
	{
		Name:      "cursor",
		Display:   "Cursor (IDE)",
		Category:  "ide",
		NpmPackage: "",
		Download:  map[string]string{
			PlatformWindows: "https://www.cursor.com/download",
			PlatformDarwin:  "https://www.cursor.com/download",
			PlatformLinux:   "https://www.cursor.com/download",
		},
		Protocol: "openai",
		Notes:    "Cursor IDE — openai-compatible provider in settings.json",
	},
	{
		Name:      "hermes",
		Display:   "Hermes CLI",
		Category:  "cli",
		NpmPackage: "",
		PipPackage: "",
		UninstallPaths: []string{".hermes"},
		LegacyBinPaths: []string{".local/bin/hermes.exe"},
		Download:  map[string]string{
			PlatformWindows: "https://hermes-agent.nousresearch.com/install.ps1",
			PlatformDarwin:  "https://hermes-agent.nousresearch.com/install.sh",
			PlatformLinux:   "https://hermes-agent.nousresearch.com/install.sh",
		},
		Protocol: "acp",
		Notes:    "Hermes Agent CLI — official installer script",
	},
	{
		Name:      "trae",
		Display:   "Trae CLI",
		Category:  "cli",
		NpmPackage: "",
		UninstallPaths: []string{".traecli"},
		Download:  map[string]string{
			PlatformWindows: "https://github.com/trae-ai/trae/releases/latest/download/trae.exe",
			PlatformDarwin:  "https://github.com/trae-ai/trae/releases/latest/download/trae",
			PlatformLinux:   "https://github.com/trae-ai/trae/releases/latest/download/trae",
		},
		Protocol: "acp",
		Notes:    "Trae CLI — ACP protocol with mcpServers config",
	},
	{
		Name:      "codebuddy",
		Display:   "CodeBuddy",
		Category:  "cli",
		NpmPackage: "",
		UninstallPaths: []string{".codebuddy"},
		Download:  map[string]string{
			PlatformWindows: "https://codebuddy.com/download",
			PlatformDarwin:  "https://codebuddy.com/download",
			PlatformLinux:   "https://codebuddy.com/download",
		},
		Protocol: "openai",
		Notes:    "CodeBuddy — download page / installer placeholder; actual CLI package to be confirmed",
	},
	{
		Name:      "copilot",
		Display:   "GitHub Copilot CLI",
		Category:  "cli",
		NpmPackage: "github/copilot-cli",
		UninstallPaths: []string{".config/github-copilot"},
		Download:  map[string]string{},
		Protocol:  "none",
		Notes:     "GitHub Copilot — npm package name placeholder; GitHub account controls provider",
	},
	{
		Name:      "deveco",
		Display:   "Deveco Studio / CLI",
		Category:  "cli",
		NpmPackage: "",
		UninstallPaths: []string{".config/deveco"},
		Download:  map[string]string{
			PlatformWindows: "https://developer.huawei.com/consumer/cn/deveco-studio",
			PlatformDarwin:  "https://developer.huawei.com/consumer/cn/deveco-studio",
			PlatformLinux:   "https://developer.huawei.com/consumer/cn/deveco-studio",
		},
		Protocol: "none",
		Notes:    "Huawei Devecode / Deveco — openai model config unavailable; own model directory",
	},
	{
		Name:      "pi",
		Display:   "Pi CLI",
		Category:  "cli",
		NpmPackage: "",
		UninstallPaths: []string{".pi"},
		Download:  map[string]string{},
		Protocol:  "none",
		Notes:     "Inflection Pi — app store / manual install; npm package no longer available",
	},
	{
		Name:      "kiro",
		Display:   "Kiro CLI",
		Category:  "cli",
		NpmPackage: "",
		UninstallPaths: []string{".kiro"},
		Download:  map[string]string{
			PlatformWindows: "https://kiro.com/download",
			PlatformDarwin:  "https://kiro.com/download",
			PlatformLinux:   "https://kiro.com/download",
		},
		Protocol: "acp",
		Notes:    "Kiro CLI — download placeholder; repo/release URL to be confirmed",
	},
	{
		Name:      "qoder",
		Display:   "Qoder CLI",
		Category:  "cli",
		NpmPackage: "",
		UninstallPaths: []string{".qoder"},
		Download:  map[string]string{
			PlatformWindows: "https://qoder.com/download",
			PlatformDarwin:  "https://qoder.com/download",
			PlatformLinux:   "https://qoder.com/download",
		},
		Protocol: "acp",
		Notes:    "Qoder CLI — download placeholder; repo/release URL to be confirmed",
	},
	{
		Name:      "grok",
		Display:   "Grok",
		Category:  "cli",
		NpmPackage: "",
		UninstallPaths: []string{".grok"},
		Download:  map[string]string{
			PlatformWindows: "https://github.com/grok/grok/releases/latest/download/grok-windows-amd64.exe",
			PlatformDarwin:  "https://github.com/grok/grok/releases/latest/download/grok-darwin-arm64",
			PlatformLinux:   "https://github.com/grok/grok/releases/latest/download/grok-linux-amd64",
		},
		Protocol: "acp",
		Notes:    "Grok — GitHub release asset placeholder; actual release filenames to be verified",
	},
	{
		Name:      "lmstudio",
		Display:   "LM Studio (CLI)",
		Category:  "cli",
		NpmPackage: "@lmstudio/sdk",
		UninstallPaths: []string{".lmstudio"},
		Download:  map[string]string{
			PlatformWindows: "https://lmstudio.ai/",
			PlatformDarwin:  "https://lmstudio.ai/",
			PlatformLinux:   "https://lmstudio.ai/",
		},
		Protocol: "openai",
		Notes:    "LM Studio CLI — openai-compatible provider, local LLM",
	},
	{
		Name:      "clawx",
		Display:   "ClawX (IDE)",
		Category:  "ide",
		NpmPackage: "",
		Download:  map[string]string{
			PlatformWindows: "https://clawx.ai/download",
		},
		Protocol: "openai",
		Notes:    "ClawX IDE — openai-compatible provider config",
	},
	{
		Name:      "gemini",
		Display:   "Gemini CLI",
		Category:  "cli",
		NpmPackage: "@google/gemini-cli",
		UninstallPaths: []string{".gemini"},
		Download:  map[string]string{},
		Protocol:  "none",
		Notes:     "Google Gemini CLI — npm package, Google auth (OAuth/API key)",
	},

}

// CurrentPlatform returns the normalized platform string
func CurrentPlatform() string {
	switch runtime.GOOS {
	case "windows":
		return PlatformWindows
	case "darwin":
		return PlatformDarwin
	default:
		return PlatformLinux
	}}

// IsCLI checks if the current platform is CLI-capable (not IDE-only)
func (a Agent) IsCLI() bool {
	return a.Category == "cli"
}

// IsIDE checks if the agent is an IDE application
func (a Agent) IsIDE() bool {
	return a.Category == "ide"
}

// InstallCommand returns the command to install this agent on the current platform.
// Returns (command, isNpm, isPip) — caller checks isNpm first (npm install -g),
// then isPip (pip install), then falls back to direct download.
func (a Agent) InstallCommand() (string, bool, bool) {
	platform := CurrentPlatform()

	// Prefer npm install if available
	if a.NpmPackage != "" {
		return fmt.Sprintf("npm install -g %s", a.NpmPackage), true, false
	}

	// Prefer pip install if available
	if a.PipPackage != "" {
		return fmt.Sprintf("pip install %s", a.PipPackage), false, true
	}

	// Fall back to direct download URL for the current platform
	if url, ok := a.Download[platform]; ok {
		return url, false, false
	}

	// Fallback: show all available download URLs
	var urls []string
	for p, url := range a.Download {
		urls = append(urls, fmt.Sprintf("%s: %s", p, url))
	}
	return "No install command available for " + platform + "\n" + strings.Join(urls, "\n"), false, false
}

// UninstallCommand returns the command to uninstall this agent on the current platform.
// Returns (command, isNpm, isPip).
func (a Agent) UninstallCommand() (string, bool, bool) {

	// Prefer npm uninstall if available
	if a.NpmPackage != "" {
		return fmt.Sprintf("npm uninstall -g %s", a.NpmPackage), true, false
	}

	// Prefer pip uninstall if available
	if a.PipPackage != "" {
		return fmt.Sprintf("pip uninstall %s", a.PipPackage), false, true
	}

	// For direct download agents, the binary may have been placed anywhere
	// by the user. Return a generic instruction rather than a download URL
	// (the install URL is not the install location on disk).
	return "请找到并删除该 agent 的二进制文件和配置文件目录（如 ~/" + a.Name + "-code/ 或 ~/" + a.Name + "/）", false, false
}

// GetUninstallPaths returns the absolute home-relative paths to delete on uninstall.
// These are directories/files relative to the user home directory.
func (a Agent) GetUninstallPaths() []string {
	return a.UninstallPaths
}

// GetLegacyBinPaths returns individual file paths to delete on uninstall (e.g. legacy binaries).
// These are paths relative to the user home directory.
func (a Agent) GetLegacyBinPaths() []string {
	return a.LegacyBinPaths
}

// UpdateCommand returns the command to update this agent on the current platform.
// For npm packages, update = re-install. For pip packages, pip install --upgrade.
// For direct download, re-download to same location.
func (a Agent) UpdateCommand() (string, bool, bool) {
	// Update is the same as install: npm install -g <package>, pip install --upgrade, or re-download
	return a.InstallCommand()
}

// HasNpmPackage checks if the agent can be installed via npm
func (a Agent) HasNpmPackage() bool {
	return a.NpmPackage != ""
}

// HasPipPackage checks if the agent can be installed via pip
func (a Agent) HasPipPackage() bool {
	return a.PipPackage != ""
}

// AllRuntimes returns the full list of installable agents
func AllRuntimes() []Agent {
	return registry
}

// GetByCategory returns agents filtered by category ("cli" or "ide")
func GetByCategory(category string) []Agent {
	var result []Agent
	for _, a := range registry {
		if a.Category == category {
			result = append(result, a)
		}
	}
	return result
}

// GetByName returns an agent by machine name, or nil if not found
func GetByName(name string) *Agent {
	for _, a := range registry {
		if a.Name == name {
			return &a
		}
	}
	return nil
}

// GetByProtocol returns agents filtered by protocol ("openai" or "acp")
func GetByProtocol(protocol string) []Agent {
	var result []Agent
	for _, a := range registry {
		if a.Protocol == protocol {
			result = append(result, a)
		}
	}
	return result
}