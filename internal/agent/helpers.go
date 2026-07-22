package agent

import (
	"os"
	"regexp"
)

// defaultModelInfo returns the default model, source label, and notes
// for a given agent. Used by all writers' StatusModel methods.
func defaultModelInfo(agentName string) (model string, source string, notes string) {
	info := map[string]struct {
		model  string
		source string
	}{
		"codex":     {"gpt-5.5", "upstream"},
		"claude":    {"fable", "proxy-map"},
		"kimi":      {"gpt-5.5", "upstream"},
		"deepseek":  {"sensenova-6.7-flash-lite", "default"},
		"opencode":  {"myccx/glm-5.2", "default"},
		"cursor":    {"sensenova-6.7-flash-lite", "default"},
		"openclaw":  {"sensenova-6.7-flash-lite", "default"},
		"openclaude":{"sensenova-6.7-flash-lite", "default"},
		"codebuddy": {"fable", "proxy-map"},
		"hermes":    {"sensenova-6.7-flash-lite", "default"},
		"kiro":      {"sensenova-6.7-flash-lite", "default"},
		"grok":      {"sensenova-6.7-flash-lite", "default"},
		"qoder":     {"sensenova-6.7-flash-lite", "default"},
		"trae":      {"sensenova-6.7-flash-lite", "default"},
	}
	v, ok := info[agentName]
	if !ok {
		return "", "default", ""
	}
	notes = ""
	if v.source == "upstream" {
		notes = "upstream 支持，直接使用"
	} else if v.source == "proxy-map" {
		notes = "上游不支持，走代理重定向"
	} else {
		notes = "上游不支持，使用默认模型（需代理重定向）"
	}
	return v.model, v.source, notes
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
