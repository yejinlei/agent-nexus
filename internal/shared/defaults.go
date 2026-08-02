// Package shared holds shared constants used by multiple packages without
// introducing import cycles (model / discover / agent).
package shared

// DefaultModels is the single authoritative source of default model names
// for every configurable agent. All other components (routing table,
// discover display, agent writer Configure() fallbacks) must use this map
// instead of defining their own copies.
//
// The key is the agent name (e.g. "codex"), the value is the model name
// that agent-nexus writes into the agent's config file.
var DefaultModels = map[string]string{
	"codex":      "gpt-5.5",
	"claude":     "fable",
	"kimi":       "gpt-5.5",
	"opencode":   "myccx/glm-5.2",
	"openclaw":   "sensenova-6.7-flash-lite",
	"openclaude": "sensenova-6.7-flash-lite",
	"hermes":     "sensenova-6.7-flash-lite",
	"kiro":       "sensenova-6.7-flash-lite",
	"grok":       "sensenova-6.7-flash-lite",
}

// GetDefaultModel returns the canonical default model name for the given
// agent. Returns ("", false) for unknown agents.
func GetDefaultModel(agentName string) (string, bool) {
	m, ok := DefaultModels[agentName]
	return m, ok
}