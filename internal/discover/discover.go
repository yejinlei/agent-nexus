package discover

import (
	"agent-nexus/internal/install"
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
	ProtocolGemini = "Gemini Native"
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
	// All configurable CLI agents accept upstream model names directly
	"codex":      ModelSourceCustom,
	"claude":     ModelSourceCustom,
	"opencode":   ModelSourceCustom,
	"openclaw":   ModelSourceCustom,
	"openclaude": ModelSourceCustom,
	"kimi":       ModelSourceCustom,
	"hermes":     ModelSourceCustom,
	"gemini":     ModelSourceCustom,
}

// nativeModelsMap lists the model names each agent accepts.
var nativeModelsMap = map[string]string{
	"codex":      "gpt-4, gpt-4o, o1, o3, deepseek-v4, glm-5, 等上游模型",
	"claude":     "claude-sonnet, claude-3.5, claude-4, gpt-4o, o1, deepseek-v4, 等上游模型",
	"opencode":   "gpt-4, gpt-4o, o1, o3, deepseek-v4, glm-5, 等上游模型",
	"openclaw":   "gpt-4, gpt-4o, o1, o3, deepseek-v4, glm-5, 等上游模型",
	"openclaude": "claude-sonnet, claude-3.5, gpt-4o, o1, deepseek-v4, 等上游模型",
	"kimi":       "gpt-4, gpt-4o, o1, deepseek-v4, glm-5, 等上游模型（通过 ACP 代理）",
	"hermes":     "gpt-4, gpt-4o, o1, deepseek-v4, glm-5, 等上游模型（通过 ACP 代理）",
	"gemini":     "Gemini 1.5, Gemini 2.0, Gemini 2.5 Flash (上游 Gemini 原生模型)",
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
	HasConfig      bool   // a configuration file was found (Stage 1/2 file, or binary in PATH)
	ConfigPath     string // absolute path to the configuration file
	IsConfigured   bool   // config file exists AND contains a known proxy URL
	IsConfigurable bool   // this agent can be configured by agent-nexus (in shared.DefaultModels)
	Protocol       string // "openai" or "acp" — the protocol the agent runtime expects
	Notes          string // human-readable note / warning
	IsInstalled    bool   // runtime itself is present: real config file, non-empty home dir, or binary in PATH
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
	BinaryName     string // binary name, checked via exec.LookupPath as fallback
	IsConfigurable bool
	Notes          string
}

var protocolMap = map[string]string{
	"codex":      ProtocolOpenAI,
	"claude":     ProtocolOpenAI,
	"kimi":       ProtocolACP,
	"opencode":   ProtocolOpenAI,
	"openclaw":   ProtocolOpenAI,
	"openclaude": ProtocolOpenAI,
	"hermes":     ProtocolACP,
	"gemini":     ProtocolGemini,
}

var registry = AgentRegistry{
	agents: []AgentPath{
		{Name: "codex", Category: "cli", Protocol: ProtocolOpenAI, ConfigFiles: []string{"Codex/config.toml"}, HomeDirFiles: []string{".codex/config.toml"}, BinaryName: "codex", IsConfigurable: true},
		{Name: "claude", Category: "cli", Protocol: ProtocolOpenAI, HomeDirFiles: []string{".claude/settings.json"}, BinaryName: "claude", IsConfigurable: true},
		{Name: "kimi", Category: "cli", Protocol: ProtocolACP, ConfigFiles: []string{".kimi/config.toml"}, HomeDirFiles: []string{".kimi-code/config.toml", ".kimi/config.toml"}, BinaryName: "kimi", IsConfigurable: true},
		{Name: "opencode", Category: "cli", Protocol: ProtocolOpenAI, ConfigFiles: []string{".config/opencode/opencode.jsonc"}, BinaryName: "opencode", IsConfigurable: true},
		{Name: "openclaw", Category: "cli", Protocol: ProtocolOpenAI, HomeDirFiles: []string{".openclaw/openclaw.json"}, BinaryName: "openclaw", IsConfigurable: true},
		{Name: "openclaude", Category: "cli", Protocol: ProtocolOpenAI, HomeDirFiles: []string{".openclaude.json", ".openclaude-env"}, BinaryName: "openclaude", IsConfigurable: true},
		{Name: "hermes", Category: "cli", Protocol: ProtocolACP, HomeDirFiles: []string{"AppData/Local/hermes/config.yaml", ".hermes/config.yaml"}, BinaryName: "hermes", IsConfigurable: true},
		{Name: "gemini", Category: "cli", Protocol: ProtocolGemini, HomeDirFiles: []string{".gemini/config.json", ".gemini/settings.json", ".gemini/.env"}, BinaryName: "gemini", IsConfigurable: false, Notes: "Google Gemini CLI, Google auth (OAuth/API key)"},
	},
}

func Discover() []AgentInfo {
	home, _ := os.UserHomeDir()
	roaming := filepath.Join(home, "AppData", "Roaming")
	results := []AgentInfo{}

	// Lookup keyed on agent name so we can still read config paths from the
	// discover registry for each discovered agent.
	byName := make(map[string]AgentPath, len(registry.agents))
	for _, ap := range registry.agents {
		byName[ap.Name] = ap
	}

	// Iterate over the authoritative 8 installable runtimes from "agent list".
	// Only those agents are scanned; everything else is excluded from discover
	// output (consistent with the --agents scope on the models commands).
	for _, r := range install.AllRuntimes() {
		ap, ok := byName[r.Name]
		if !ok {
			continue
		}

		var configPath string
		var fileFound bool   // a real config FILE was found (Stage 1/2)
		var isInstalled bool // the runtime itself is present on disk/PATH

		for _, rel := range ap.HomeDirFiles {
			p := filepath.Join(home, rel)
			if _, err := os.Stat(p); err == nil {
				configPath = p
				fileFound = true
				isInstalled = true
				break
			}
		}

		if !fileFound {
			for _, rel := range ap.ConfigFiles {
				p := filepath.Join(roaming, rel)
				if _, err := os.Stat(p); err == nil {
					configPath = p
					fileFound = true
					break
				}
			}
		}

		// Fallback: when no config file exists but the agent is installed,
		// set configPath to the expected config file (needed by conf set to
		// create/write the file). Check by directory existence, not file.
		if !fileFound && ap.IsConfigurable {
			var candidate string
			if len(ap.HomeDirFiles) > 0 {
				candidate = filepath.Join(home, ap.HomeDirFiles[0])
			} else if len(ap.ConfigFiles) > 0 {
				candidate = filepath.Join(roaming, ap.ConfigFiles[0])
			}
			if candidate != "" && r.UninstallPaths != nil {
				for _, rel := range r.UninstallPaths {
					p := filepath.Join(home, rel)
					if info, err := os.Stat(p); err == nil && info.IsDir() {
						// Directory exists (agent installed). Set the expected
						// config file path so conf set can write to it.
						configPath = candidate
						fileFound = true
						break
					}
				}
			}
			// If no uninstall path but we have a binary, still set the
			// expected config path so conf set can create the file.
			if !fileFound && ap.BinaryName != "" && candidate != "" {
				configPath = candidate
				fileFound = true
			}
		}

		info := AgentInfo{
			Name:           r.Name,
			Category:       r.Category,
			HasConfig:      fileFound,
			ConfigPath:     configPath,
			IsConfigured:   false,
			IsConfigurable: ap.IsConfigurable,
			Protocol:       protocolMap[r.Name],
			Notes:          ap.Notes,
			IsInstalled:    isInstalled,
		}

		if fileFound && ap.IsConfigurable {
			info.IsConfigured = checkConfigured(configPath)
		}

		if !info.HasConfig && ap.BinaryName != "" {
			if _, err := exec.LookPath(ap.BinaryName); err == nil {
				info.HasConfig = true
				info.IsInstalled = true
			}
		}

		info.IsInstalled = info.IsInstalled || fileFound

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
		strings.Contains(content, "localhost:11434")
}

func GetRegistry() []AgentPath {
	return registry.agents
}

// ProtocolMap returns a copy of the per-agent protocol labels.
func ProtocolMap() map[string]string {
	m := make(map[string]string, len(protocolMap))
	for k, v := range protocolMap {
		m[k] = v
	}
	return m
}

// ModelSourceMap returns a copy of the per-agent ModelSource classification.
func ModelSourceMap() map[string]ModelSource {
	m := make(map[string]ModelSource, len(modelSourceMap))
	for k, v := range modelSourceMap {
		m[k] = v
	}
	return m
}

// NativeModelsMap returns a copy of the per-agent native model descriptions.
func NativeModelsMap() map[string]string {
	m := make(map[string]string, len(nativeModelsMap))
	for k, v := range nativeModelsMap {
		m[k] = v
	}
	return m
}

// IsConfigurableMap returns a copy of the per-agent configurable flag.
func IsConfigurableMap() map[string]bool {
	m := make(map[string]bool, len(registry.agents))
	for _, a := range registry.agents {
		m[a.Name] = a.IsConfigurable
	}
	return m
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
