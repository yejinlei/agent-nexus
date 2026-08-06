// Package shared holds shared constants used by multiple packages without
// introducing import cycles (model / discover / agent).
package shared

// TierModels carries the per-role model names for a single agent.
//
// For agents that expose multiple model slots (e.g. Claude's opus/sonnet/haiku
// tiers, or OpenCode's primary/small_model roles) each field names the upstream
// model that role resolves to. Fields left empty mean "not applicable for this
// agent" and callers fall back to Default.
type TierModels struct {
	Default string `json:"default"` // fallback / single-model agents
	Opus    string `json:"opus"`
	Sonnet  string `json:"sonnet"`
	Haiku   string `json:"haiku"`
}

// Single returns the Default model. It is the backward-compatible view that
// matches the previous map[string]string API.
func (t TierModels) Single() string {
	return t.Default
}

// DefaultModels is the single authoritative source of model names for every
// configurable agent. All other components (routing table, discover display,
// agent writer Configure() fallbacks) must use this map instead of defining
// their own copies.
//
// The key is the agent name (e.g. "codex"). The value's Default field is the
// single model name used by every code path that has not yet been made
// tier-aware; the Opus/Sonnet/Haiku fields hold the per-tier model names for
// agents that support them (currently only claude).
var DefaultModels = map[string]TierModels{
	"codex":      {Default: "gpt-5.5"},
	"claude":     {Default: "fable", Opus: "claude-opus-4", Sonnet: "claude-sonnet-5", Haiku: "claude-haiku-4.5"},
	"kimi":       {Default: "gpt-5.5"},
	"opencode":   {Default: "myccx/glm-5.2"},
	"openclaw":   {Default: "sensenova-6.7-flash-lite"},
	"openclaude": {Default: "sensenova-6.7-flash-lite"},
	"hermes":     {Default: "sensenova-6.7-flash-lite"},
	"gemini":     {Default: "sensenova-6.7-flash-lite"},
}

// GetDefaultModel returns the canonical single-model default for the given
// agent. This is the backward-compatible view over the per-tier map.
func GetDefaultModel(agentName string) (string, bool) {
	t, ok := DefaultModels[agentName]
	return t.Default, ok
}

// GetTierModels returns the full per-tier model set for the given agent.
// Returns (TierModels{}, false) for unknown agents.
func GetTierModels(agentName string) (TierModels, bool) {
	t, ok := DefaultModels[agentName]
	return t, ok
}
