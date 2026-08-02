package agent

import (
	"os"
	"regexp"

	"agent-nexus/internal/shared"
)

// defaultSourceAndNotes holds the (source, notes) pair for each agent whose
// default model requires non-trivial routing. The default model name itself is
// looked up from the central shared.DefaultModels map via shared.GetDefaultModel.
//
// Source semantics (consistent with ResolveModelForAgent):
//   "upstream"    = the agent's default model is normally available upstream.
//   "proxy-map"   = the agent's default model needs proxy model-redefinition.
//   "default"     = the agent's default model needs proxy redirect as-is.
var defaultSourceAndNotes = map[string]struct {
	source string
	notes  string
}{
	"codex":     {"upstream", "upstream 支持，直接使用"},
	"claude":    {"proxy-map", "上游不支持，走代理重定向"},
	"kimi":      {"upstream", "upstream 支持，直接使用"},
	"deepseek":  {"default", "上游不支持，使用默认模型（需代理重定向）"},
	"opencode":  {"default", "上游不支持，使用默认模型（需代理重定向）"},
	"cursor":    {"default", "上游不支持，使用默认模型（需代理重定向）"},
	"openclaw":  {"default", "上游不支持，使用默认模型（需代理重定向）"},
	"openclaude": {"default", "上游不支持，使用默认模型（需代理重定向）"},
	"codebuddy": {"proxy-map", "上游不支持，走代理重定向"},
	"hermes":    {"default", "上游不支持，使用默认模型（需代理重定向）"},
	"qoder":     {"default", "上游不支持，使用默认模型（需代理重定向）"},
	"trae":      {"default", "上游不支持，使用默认模型（需代理重定向）"},
}

// defaultModelInfo returns the default model, source label, and notes for a
// given agent. The model name is resolved from the central DefaultModels map;
// the source/notes are taken from the local table above (kept for display
// consistency with ResolveModelForAgent).
func defaultModelInfo(agentName string) (model string, source string, notes string) {
	m, ok := shared.GetDefaultModel(agentName)
	info, has := defaultSourceAndNotes[agentName]
	if !ok {
		if !has {
			return "", "default", ""
		}
		return "", info.source, info.notes
	}
	return m, info.source, info.notes
}

// extractModelFromConfig reads the model field from a config file (TOML or JSON).
// Returns (model, found).
func extractModelFromConfig(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	s := string(data)
	// TOML: model = "xxx"
	re := regexp.MustCompile(`model\s*=\s*"([^"]+)"`)
	matches := re.FindStringSubmatch(s)
	if len(matches) > 1 {
		return matches[1], true
	}
	// JSON: "model": "xxx"
	re2 := regexp.MustCompile(`"model"\s*:\s*"([^"]+)"`)
	matches = re2.FindStringSubmatch(s)
	if len(matches) > 1 {
		return matches[1], true
	}
	return "", false
}

// ExtractConfiguredModel reads the currently configured model name from an agent's
// config file, handling TOML, JSON, and YAML formats.
// Returns (model, found).
func ExtractConfiguredModel(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	s := string(data)

	// TOML: model = "xxx"
	re := regexp.MustCompile(`model\s*=\s*"([^"]+)"`)
	if m := re.FindStringSubmatch(s); len(m) > 1 {
		return m[1], true
	}
	// YAML: default_model: xxx
	re2 := regexp.MustCompile(`(?i)default_model:\s*(\S+)`)
	if m := re2.FindStringSubmatch(s); len(m) > 1 {
		return m[1], true
	}
	// JSON: "model": "xxx"
	re3 := regexp.MustCompile(`"model"\s*:\s*"([^"]+)"`)
	if m := re3.FindStringSubmatch(s); len(m) > 1 {
		return m[1], true
	}
	return "", false
}
// modelDefault returns the canonical default model for this writer's agent
// from the central shared.DefaultModels map.
func modelDefault(agentName string) string {
	m, ok := shared.GetDefaultModel(agentName)
	if !ok {
		return ""
	}
	return m
}