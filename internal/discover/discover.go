package discover

import (
	"agent-nexus/internal/shared"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	ProtocolOpenAI = "OpenAI Compatible"
	ProtocolACP    = "ACP"
	ProtocolNone   = "N/A"
)

// ModelSource classifies how an agent obtains its model.
type ModelSource int

const (
	ModelSourceCustom   ModelSource = iota // OpenAI/compatible: accepts any upstream model name directly
	ModelSourceRedirect                    // ACP: needs proxy to redirect upstream model → native model name
	ModelSourceOwn                         // Own backend: doesn't use proxy
)

// ModelSourceLabel returns a human-readable label for a ModelSource.
func ModelSourceLabel(src ModelSource) string {
	switch src {
	case ModelSourceCustom:
		return "自定义模型"
	case ModelSourceRedirect:
		return "需重定向"
	case ModelSourceOwn:
		return "自有模型"
	default:
		return "N/A"
	}
}

// modelSourceMap classifies each agent's model source.
var modelSourceMap = map[string]ModelSource{
	// OpenAI Compatible: supports custom model (accepts any upstream model name)
	"codex":      ModelSourceCustom,
	"claude":     ModelSourceCustom,
	"deepseek":   ModelSourceCustom,
	"opencode":   ModelSourceCustom,
	"openclaw":   ModelSourceCustom,
	"openclaude": ModelSourceCustom,
	"cursor":     ModelSourceCustom,
	"codebuddy":  ModelSourceCustom,
	"lmstudio":   ModelSourceCustom,
	"clawx":      ModelSourceCustom,
	// ACP: needs model redirect (proxy maps upstream → native model name)
	"kimi":   ModelSourceRedirect,
	"hermes": ModelSourceRedirect,
	"kiro":   ModelSourceRedirect,
	"grok":   ModelSourceRedirect,
	"qoder":  ModelSourceRedirect,
	"trae":   ModelSourceRedirect,
	// Own backend / non-configurable
	"antigravity":   ModelSourceOwn,
	"copilot":       ModelSourceOwn,
	"pi":            ModelSourceOwn,
	"deveco":        ModelSourceOwn,
	"qoder-ide":     ModelSourceOwn,
	"trae-ide":      ModelSourceOwn,
	"codebuddy-ide": ModelSourceOwn,
	"windsurf":      ModelSourceOwn,
	"zed":           ModelSourceOwn,
	"gemini":        ModelSourceOwn,
}

// nativeModelsMap lists the model names each agent accepts.
// For custom-model agents: these are representative upstream model names.
// For redirect agents: these are the native model names the agent understands.
var nativeModelsMap = map[string]string{
	"codex":         "gpt-4, gpt-4o, o1, o3, claude-sonnet, claude-3.5, deepseek-v4, glm-5, 等上游模型",
	"claude":        "claude-sonnet, claude-3.5, claude-4, gpt-4o, o1, deepseek-v4, 等上游模型",
	"deepseek":      "deepseek-v4, deepseek-v4-flash, deepseek-coder, gpt-4o, o1, claude-sonnet, 等上游模型",
	"opencode":      "gpt-4, gpt-4o, o1, o3, claude-sonnet, claude-3.5, deepseek-v4, 等上游模型",
	"openclaw":      "gpt-4, gpt-4o, o1, o3, claude-sonnet, claude-3.5, deepseek-v4, 等上游模型",
	"openclaude":    "claude-sonnet, claude-3.5, gpt-4o, o1, deepseek-v4, 等上游模型",
	"cursor":        "gpt-4, gpt-4o, o1, o3, claude-sonnet, claude-3.5, deepseek-v4, 等上游模型",
	"codebuddy":     "claude-sonnet, claude-3.5, gpt-4o, o1, deepseek-v4, 等上游模型",
	"lmstudio":      "本地 LLM (通过 localhost 加载的任意模型，如 llama, qwen, mistral 等)",
	"clawx":         "gpt-4, gpt-4o, o1, o3, claude-sonnet, claude-3.5, deepseek-v4, 等上游模型",
	"kimi":          "Kimi K1, Kimi K2, Kimi-Max (通过代理路由到上游模型)",
	"hermes":        "Hermes 2, Hermes 3 (通过代理路由到上游模型)",
	"kiro":          "Kiro 原生模型 (通过代理路由到上游模型)",
	"grok":          "Grok 2, Grok 3 (通过代理路由到上游模型)",
	"qoder":         "Qoder 原生模型 (通过代理路由到上游模型)",
	"trae":          "Trae-Plus, Trae-Code (通过代理路由到上游模型)",
	"antigravity":   "Gemini 1.5, Gemini 1.7, Gemini 2.0, Gemini Flash (Google Gemini)",
	"copilot":       "GPT-4, Claude 等 (由 GitHub 账号控制)",
	"pi":            "Inflection Pi (Pi CLI 自有模型)",
	"deveco":        "华为大模型 (自有模型目录)",
	"qoder-ide":     "自有 AI 后端 (具体模型由 agent 内部决定)",
	"trae-ide":      "自有 AI 后端 (具体模型由 agent 内部决定)",
	"codebuddy-ide": "自有 AI 后端 (具体模型由 agent 内部决定)",
	"windsurf":      "自有 AI 后端 (具体模型由 agent 内部决定)",
	"zed":           "无内置 AI Agent (N/A)",
	"gemini":        "Gemini 1.5, Gemini 1.7, Gemini 2.0, Gemini Flash (通过 Google OAuth)",
}

// ModelSourceForAgent returns the model source classification for an agent.
func ModelSourceForAgent(agentName string) ModelSource {
	if src, ok := modelSourceMap[agentName]; ok {
		return src
	}
	return ModelSourceOwn
}

// NativeModelsForAgent returns the list of model names an agent accepts.
func NativeModelsForAgent(agentName string) string {
	if v, ok := nativeModelsMap[agentName]; ok {
		return v
	}
	return "N/A"
}

// RenderModelTable renders the agent models table with 6 columns.
// Columns: Agent | 类型 | 协议 | 模型来源 | 模型列表 | 说明
func RenderModelTable(agents []AgentInfo) {
	if len(agents) == 0 {
		fmt.Println("未发现任何 agent。")
		return
	}

	fmt.Printf("Agent 原生支持的模型 (%d 个):\n\n", len(agents))
	fmt.Println(strings.Repeat("-", 150))

	colAgent := "Agent"
	colType := "类型"
	colProto := "协议"
	colSrc := "模型来源"
	colModels := "模型列表"
	colNotes := "说明"

	widthAgent := maxStrWidth(append([]string{colAgent}, func() []string {
		names := make([]string, len(agents))
		for i, a := range agents {
			names[i] = a.Name
		}
		return names
	}()...))
	widthType := maxStrWidth([]string{colType, "cli", "ide"})
	widthProto := maxStrWidth(append([]string{colProto}, func() []string {
		protos := make([]string, len(agents))
		for i, a := range agents {
			protos[i] = a.Protocol
		}
		return protos
	}()...))
	widthSrc := maxStrWidth(append([]string{colSrc}, func() []string {
		sources := make([]string, len(agents))
		for i, a := range agents {
			sources[i] = ModelSourceLabel(ModelSourceForAgent(a.Name))
		}
		return sources
	}()...))
	widthModels := maxStrWidth(append([]string{colModels}, func() []string {
		models := make([]string, len(agents))
		for i, a := range agents {
			models[i] = NativeModelsForAgent(a.Name)
		}
		return models
	}()...))

	fmt.Printf("  %-*s  %-*s  %-*s  %-*s  %-*s  %s\n",
		widthAgent, colAgent,
		widthType, colType,
		widthProto, colProto,
		widthSrc, colSrc,
		widthModels, colModels,
		colNotes)
	fmt.Printf("  %s  %s  %s  %s  %s  %s\n",
		strings.Repeat("-", widthAgent),
		strings.Repeat("-", widthType),
		strings.Repeat("-", widthProto),
		strings.Repeat("-", widthSrc),
		strings.Repeat("-", widthModels),
		"")

	for _, a := range agents {
		src := ModelSourceForAgent(a.Name)
		models := NativeModelsForAgent(a.Name)
		notes := a.Notes
		if notes == "" {
			switch src {
			case ModelSourceCustom:
				notes = "可直接使用上游网关任何模型"
			case ModelSourceRedirect:
				notes = "需代理将上游模型映射为可识别名"
			case ModelSourceOwn:
				notes = "不通过代理，使用自有后端"
			}
		}
		fmt.Printf("  %-*s  %-*s  %-*s  %-*s  %-*s  %s\n",
			widthAgent, a.Name,
			widthType, a.Category,
			widthProto, a.Protocol,
			widthSrc, ModelSourceLabel(src),
			widthModels, models,
			notes)
	}
	fmt.Println()
}

type AgentInfo struct {
	Name           string
	Category       string
	HasConfig      bool
	ConfigPath     string
	IsConfigured   bool
	IsConfigurable bool
	Protocol       string
	Notes          string
}

type AgentRegistry struct {
	agents []AgentPath
}

type AgentPath struct {
	Name           string
	Category       string
	Protocol       string
	ConfigFiles    []string
	HomeDirFiles   []string
	BinaryName     string // npm binary name, checked via exec.LookPath as fallback
	IsConfigurable bool
	Notes          string
}

var protocolMap = map[string]string{
	"codex":         ProtocolOpenAI,
	"claude":        ProtocolOpenAI,
	"kimi":          ProtocolACP,
	"deepseek":      ProtocolOpenAI,
	"opencode":      ProtocolOpenAI,
	"openclaw":      ProtocolOpenAI,
	"openclaude":    ProtocolOpenAI,
	"cursor":        ProtocolOpenAI,
	"codebuddy":     ProtocolOpenAI,
	"hermes":        ProtocolACP,
	"kiro":          ProtocolACP,
	"grok":          ProtocolACP,
	"qoder":         ProtocolACP,
	"trae":          ProtocolACP,
	"antigravity":   ProtocolNone,
	"copilot":       ProtocolNone,
	"pi":            ProtocolNone,
	"deveco":        ProtocolNone,
	"qoder-ide":     ProtocolNone,
	"trae-ide":      ProtocolNone,
	"codebuddy-ide": ProtocolNone,
	"windsurf":      ProtocolNone,
	"zed":           ProtocolNone,
	"lmstudio":      ProtocolOpenAI,
	"clawx":         ProtocolOpenAI,
	"gemini":        ProtocolNone,
}

var registry = AgentRegistry{
	agents: []AgentPath{
		{Name: "codex", Category: "cli", Protocol: ProtocolOpenAI, ConfigFiles: []string{"Codex/config.toml"}, HomeDirFiles: []string{".codex/config.toml"}, BinaryName: "codex", IsConfigurable: true},
		{Name: "claude", Category: "cli", Protocol: ProtocolOpenAI, ConfigFiles: []string{"Claude/settings.json"}, HomeDirFiles: []string{".claude/settings.json"}, BinaryName: "claude", IsConfigurable: true},
		{Name: "kimi", Category: "cli", Protocol: ProtocolACP, ConfigFiles: []string{".kimi/config.toml"}, HomeDirFiles: []string{".kimi-code/config.toml", ".kimi/config.toml"}, BinaryName: "kimi", IsConfigurable: true},
		{Name: "deepseek", Category: "cli", Protocol: ProtocolOpenAI, HomeDirFiles: []string{".deepseek/config.toml"}, IsConfigurable: true},
		{Name: "opencode", Category: "cli", Protocol: ProtocolOpenAI, ConfigFiles: []string{".config/opencode/opencode.jsonc"}, IsConfigurable: true},
		{Name: "openclaw", Category: "cli", Protocol: ProtocolOpenAI, HomeDirFiles: []string{".openclaw/openclaw.json"}, IsConfigurable: true},
		{Name: "openclaude", Category: "cli", Protocol: ProtocolOpenAI, HomeDirFiles: []string{".openclaude-env"}, BinaryName: "openclaude", IsConfigurable: true},
		{Name: "cursor", Category: "ide", Protocol: ProtocolOpenAI, ConfigFiles: []string{"Cursor/User/settings.json"}, IsConfigurable: true},
		{Name: "codebuddy", Category: "cli", Protocol: ProtocolOpenAI, HomeDirFiles: []string{".codebuddy/settings.json"}, IsConfigurable: true},
		{Name: "hermes", Category: "cli", Protocol: ProtocolACP, HomeDirFiles: []string{".hermes/config.yaml"}, IsConfigurable: true},
		{Name: "kiro", Category: "cli", Protocol: ProtocolACP, HomeDirFiles: []string{".kiro/config.yaml"}, IsConfigurable: true},
		{Name: "grok", Category: "cli", Protocol: ProtocolACP, HomeDirFiles: []string{".grok/config.yaml"}, IsConfigurable: true},
		{Name: "qoder", Category: "cli", Protocol: ProtocolACP, HomeDirFiles: []string{".qoder/config.yaml"}, IsConfigurable: true},
		{Name: "trae", Category: "cli", Protocol: ProtocolACP, HomeDirFiles: []string{".traecli/config.yaml"}, IsConfigurable: true},
		{Name: "antigravity", Category: "cli", Protocol: ProtocolNone, HomeDirFiles: []string{".agents/config.yaml"}, IsConfigurable: false, Notes: "Google Gemini, no external model config"},
		{Name: "copilot", Category: "cli", Protocol: ProtocolNone, ConfigFiles: []string{".config/github-copilot/config.yaml"}, IsConfigurable: false, Notes: "GitHub account determines model"},
		{Name: "pi", Category: "cli", Protocol: ProtocolNone, HomeDirFiles: []string{".pi/agent/settings.json"}, BinaryName: "pi", IsConfigurable: true, Notes: "Inflection Pi CLI (npm: @earendil-works/pi-coding-agent)"},
		{Name: "deveco", Category: "cli", Protocol: ProtocolNone, ConfigFiles: []string{".config/deveco/deveco.jsonc"}, IsConfigurable: false, Notes: "Huawei OpenCode engine, own model directory"},
		{Name: "qoder-ide", Category: "ide", Protocol: ProtocolNone, ConfigFiles: []string{"Qoder/User/settings.json"}, IsConfigurable: false, Notes: "Own AI backend"},
		{Name: "trae-ide", Category: "ide", Protocol: ProtocolNone, ConfigFiles: []string{"Trae/User/settings.json"}, IsConfigurable: false, Notes: "Own AI backend"},
		{Name: "codebuddy-ide", Category: "ide", Protocol: ProtocolNone, ConfigFiles: []string{"CodeBuddy/User/settings.json"}, IsConfigurable: false, Notes: "Own AI backend"},
		{Name: "windsurf", Category: "ide", Protocol: ProtocolNone, ConfigFiles: []string{"Windsurf/User/settings.json"}, IsConfigurable: false, Notes: "Own AI backend"},
		{Name: "zed", Category: "ide", Protocol: ProtocolNone, ConfigFiles: []string{"Zed/settings.json"}, IsConfigurable: false, Notes: "No built-in AI Agent"},
		{Name: "lmstudio", Category: "cli", Protocol: ProtocolOpenAI, ConfigFiles: []string{"LM Studio/settings.json"}, IsConfigurable: true},
		{Name: "clawx", Category: "ide", Protocol: ProtocolOpenAI, HomeDirFiles: []string{"AppData/Roaming/clawx/clawx-providers.json"}, IsConfigurable: true},
		{Name: "gemini", Category: "cli", Protocol: ProtocolNone, HomeDirFiles: []string{".gemini/config.json"}, IsConfigurable: false, Notes: "Google Gemini CLI, Google auth (OAuth/API key)"},
	},
}

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

		if strings.HasSuffix(ap.Name, "-ide") {
			continue
		}
		info := AgentInfo{
			Name:           ap.Name,
			Category:       ap.Category,
			HasConfig:      found,
			ConfigPath:     configPath,
			IsConfigurable: ap.IsConfigurable,
			Protocol:       protocolMap[ap.Name],
			Notes:          ap.Notes,
		}

		if found && ap.IsConfigurable {
			info.IsConfigured = checkConfigured(configPath)
		}

		if !info.HasConfig && ap.BinaryName != "" {
			if _, err := exec.LookPath(ap.BinaryName); err == nil {
				info.HasConfig = true
			}
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
	return strings.Contains(content, "127.0.0.1") ||
		strings.Contains(content, "sensenova") ||
		strings.Contains(content, "platform.sensenova") ||
		strings.Contains(content, "api.deepseek") ||
		strings.Contains(content, "api.siliconflow") ||
		strings.Contains(content, "localhost:11444")
}

func GetRegistry() []AgentPath {
	return registry.agents
}

// ProtocolExamples returns the model families natively supported by an agent's
// protocol or built-in AI backend. This is independent of any proxy or config.
// DEPRECATED: Use ModelSourceForAgent + NativeModelsForAgent instead.
func ProtocolExamples(agentName string) (modelFamilies string, examples string) {
	v := NativeModelsForAgent(agentName)
	return v, ""
}

func RenderTable(agents []AgentInfo) {
	if len(agents) == 0 {
		fmt.Println("No AI agents found.")
		return
	}

	fmt.Printf("\nDiscovered %d AI agents:\n\n", len(agents))

	colName := "Agent"
	colCat := "Type"
	colProtocol := "Protocol"
	colStatus := "Status"
	colConfig := "Configured"

	widthName := maxStrWidth(append(append([]string{colName}, agentNames(agents)...), ""))
	widthCat := maxStrWidth(append([]string{colCat}, agentCats(agents)...))
	widthProtocol := maxStrWidth(append([]string{colProtocol}, agentProtocols(agents)...))
	widthStatus := maxStrWidth(append([]string{colStatus}, agentStatuses(agents)...))
	widthConfig := maxStrWidth(append([]string{colConfig}, agentConfigStatuses(agents)...))

	fmt.Printf("  %-*s  %-*s  %-*s  %-*s  %-*s  %s\n",
		widthName, colName,
		widthCat, colCat,
		widthProtocol, colProtocol,
		widthStatus, colStatus,
		widthConfig, colConfig,
		"Config Path")

	fmt.Printf("  %s  %s  %s  %s  %s  %s\n",
		strings.Repeat("-", widthName),
		strings.Repeat("-", widthCat),
		strings.Repeat("-", widthProtocol),
		strings.Repeat("-", widthStatus),
		strings.Repeat("-", widthConfig),
		"")

	for _, a := range agents {
		installed := "Installed"
		if !a.HasConfig {
			installed = "Not installed"
		}
		configured := "Yes"
		if !a.IsConfigured {
			configured = "No"
		}
		if !a.IsConfigurable {
			configured = "-"
		}
		pathDisplay := a.ConfigPath
		if !a.HasConfig {
			pathDisplay = "-"
		}
		if a.HasConfig && a.ConfigPath == "" {
			pathDisplay = "(via npm, no config yet)"
		}

		fmt.Printf("  %-*s  %-*s  %-*s  %-*s  %-*s  %s\n",
			widthName, a.Name,
			widthCat, a.Category,
			widthProtocol, a.Protocol,
			widthStatus, installed,
			widthConfig, configured,
			pathDisplay)
	}
	fmt.Println()
}

func RenderVerboseTable(agents []AgentInfo) {
	colAgent := "Agent"
	colCat := "Type"
	colProtocol := "Protocol"
	colStatus := "Status"
	colConfig := "Configured"
	colDefault := "Default Model"
	colRouted := "Routed To"
	colCustom := "Custom Model"

	widthAgent := maxStrWidth(append([]string{colAgent}, agentNames(agents)...))
	widthCat := maxStrWidth([]string{colCat, "cli", "ide"})
	widthProto := maxStrWidth(append([]string{colProtocol}, agentProtocols(agents)...))
	widthStatus := maxStrWidth(append([]string{colStatus}, agentVerboseStatuses(agents)...))
	widthConfig := maxStrWidth([]string{colConfig, "Yes", "No", "-"})
	widthDef := maxStrWidth(append([]string{colDefault}, agentDefaultModels(agents)...))
	widthRouted := maxStrWidth(append([]string{colRouted}, agentRoutedModels(agents)...))
	widthCustom := maxStrWidth(append([]string{colCustom}, agentCustomSupport(agents)...))

	fmt.Printf("  %-*s  %-*s  %-*s  %-*s  %-*s  %-*s  %-*s  %-*s\n",
		widthAgent, colAgent,
		widthCat, colCat,
		widthProto, colProtocol,
		widthStatus, colStatus,
		widthConfig, colConfig,
		widthDef, colDefault,
		widthRouted, colRouted,
		widthCustom, colCustom)

	fmt.Printf("  %s  %s  %s  %s  %s  %s  %s  %s\n",
		strings.Repeat("-", widthAgent),
		strings.Repeat("-", widthCat),
		strings.Repeat("-", widthProto),
		strings.Repeat("-", widthStatus),
		strings.Repeat("-", widthConfig),
		strings.Repeat("-", widthDef),
		strings.Repeat("-", widthRouted),
		strings.Repeat("-", widthCustom))

	for _, a := range agents {
		installed := "Installed"
		if !a.HasConfig {
			installed = "Not installed"
		}
		configured := "Yes"
		if !a.IsConfigured {
			configured = "No"
		}
		if !a.IsConfigurable {
			configured = "-"
		}
		custom := "Yes"
		if !a.IsConfigurable {
			custom = "-"
		}
		fmt.Printf("  %-*s  %-*s  %-*s  %-*s  %-*s  %-*s  %-*s  %-*s\n",
			widthAgent, a.Name,
			widthCat, a.Category,
			widthProto, a.Protocol,
			widthStatus, installed,
			widthConfig, configured,
			widthDef, AgentDefaultModel(a.Name),
			widthRouted, AgentDefaultModel(a.Name),
			widthCustom, custom)
	}
	fmt.Println()
}

func agentNames(agents []AgentInfo) []string {
	names := make([]string, len(agents))
	for i, a := range agents {
		names[i] = a.Name
	}
	return names
}

func agentCats(agents []AgentInfo) []string {
	cats := make([]string, len(agents))
	for i, a := range agents {
		cats[i] = a.Category
	}
	return cats
}

func agentProtocols(agents []AgentInfo) []string {
	protos := make([]string, len(agents))
	for i, a := range agents {
		protos[i] = a.Protocol
	}
	return protos
}

func agentStatuses(agents []AgentInfo) []string {
	statuses := make([]string, 0, len(agents))
	for _, a := range agents {
		statuses = append(statuses, agentStatus(a))
	}
	return statuses
}

func agentStatus(a AgentInfo) string {
	if !a.HasConfig {
		return "Not installed"
	}
	return "Installed"
}

func agentConfigStatuses(agents []AgentInfo) []string {
	statuses := make([]string, 0, len(agents))
	for _, a := range agents {
		statuses = append(statuses, agentConfigStatus(a))
	}
	return statuses
}

func agentConfigStatus(a AgentInfo) string {
	if !a.IsConfigurable {
		return "-"
	}
	if a.IsConfigured {
		return "Yes"
	}
	return "No"
}

func agentVerboseStatuses(agents []AgentInfo) []string {
	statuses := make([]string, 0, len(agents))
	for _, a := range agents {
		statuses = append(statuses, agentStatus(a))
	}
	return statuses
}

func agentDefaultModels(agents []AgentInfo) []string {
	models := make([]string, len(agents))
	for i, a := range agents {
		models[i] = AgentDefaultModel(a.Name)
	}
	return models
}

func agentRoutedModels(agents []AgentInfo) []string {
	models := make([]string, len(agents))
	for i, a := range agents {
		models[i] = AgentDefaultModel(a.Name)
	}
	return models
}

func agentCustomSupport(agents []AgentInfo) []string {
	support := make([]string, len(agents))
	for i, a := range agents {
		support[i] = agentCustomIcon(a)
	}
	return support
}

func agentCustomIcon(a AgentInfo) string {
	if a.IsConfigurable {
		return "Yes"
	}
	return "-"
}

func AgentDefaultModel(name string) string {
	// Single source of truth: shared.GetDefaultModel (was duplicated in 4 places)
	m, ok := shared.GetDefaultModel(name)
	if !ok {
		return "N/A"
	}
	return m
}
func maxStrWidth(strs []string) int {
	maxW := 0
	for _, s := range strs {
		w := 0
		for _, r := range s {
			if r > 0x2E7F && (r <= 0x9FFF || r >= 0xF900 && r <= 0xFAFF || r >= 0x3400 && r <= 0x4DBF || r >= 0x20000 && r <= 0x2A6DF) {
				w += 2
			} else {
				w++
			}
		}
		if w > maxW {
			maxW = w
		}
	}
	return maxW
}
