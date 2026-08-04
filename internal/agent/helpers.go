package agent

import (
	"fmt"
	"os"
	"regexp"

	"agent-nexus/internal/shared"
)

// ErrProtocolIncompatible is returned when the upstream endpoint does not
// support the protocol required by a particular agent (e.g. Codex needs the
// OpenAI Responses API /v1/responses but the endpoint only serves
// chat/completions or messages).
type ErrProtocolIncompatible struct {
	Agent    string
	BaseURL  string
	Reason   string
	Fallback string
}

func (e *ErrProtocolIncompatible) Error() string {
	msg := fmt.Sprintf("协议不兼容: %s 需要 %s，但 %s 不支持", e.Agent, e.Reason, e.BaseURL)
	if e.Fallback != "" {
		msg += fmt.Sprintf("；建议: %s", e.Fallback)
	}
	return msg
}

// IsProtocolIncompatible reports whether err is an ErrProtocolIncompatible.
func IsProtocolIncompatible(err error) bool {
	_, ok := err.(*ErrProtocolIncompatible)
	return ok
}

// defaultSourceAndNotes holds the (source, notes) pair for each agent whose
// default model requires non-trivial routing. The default model name itself is
// looked up from the central shared.DefaultModels map via shared.GetDefaultModel.
//
// Source semantics (consistent with ResolveModelForAgent):
//
//	"upstream"    = the agent's default model is normally available upstream.
//	"proxy-map"   = the agent's default model needs proxy model-redefinition.
//	"default"     = the agent's default model needs proxy redirect as-is.
var defaultSourceAndNotes = map[string]struct {
	source string
	notes  string
}{
	"codex":      {"upstream", "upstream 支持，直接使用"},
	"claude":     {"proxy-map", "上游不支持，走代理重定向"},
	"kimi":       {"upstream", "upstream 支持，直接使用"},
	"opencode":   {"upstream", "upstream 支持，直接使用"},
	"openclaw":   {"upstream", "upstream 支持，直接使用"},
	"openclaude": {"upstream", "upstream 支持，直接使用"},
	"hermes":     {"default", "使用上游模型名"},
	"gemini":     {"upstream", "使用上游 Gemini 原生模型名"},
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
	// YAML: default_model: xxx  (unquoted) or default_model: "xxx" (quoted)
	re2 := regexp.MustCompile(`(?i)default_model:\s*["']?([^"'\s]+)["']?`)
	if m := re2.FindStringSubmatch(s); len(m) > 1 {
		return m[1], true
	}
	// JSON: "model": "xxx"  (key itself may carry a trailing comma is fine;
	// also match single-quoted keys/values found in some configs)
	re3 := regexp.MustCompile(`["']model["']\s*:\s*["']([^"']*)["']`)
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
