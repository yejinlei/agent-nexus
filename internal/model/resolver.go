package model

import (
	"sort"
	"strings"

	"agent-nexus/internal/db"
	"agent-nexus/internal/proxy"
	"agent-nexus/internal/shared"
)

// DefaultModels is the single authoritative source of default model names
// for every configurable agent. All other components (routing table,
// discover display, agent writer Configure() fallbacks) must use this map
// instead of defining their own copies.
//
// The key is the agent name (e.g. "codex"), the value is the model name
// that agent-nexus writes into the agent's config file.

// ResolveModelForAgent determines the best model to use for a given agent.
//
// Logic:
//  1. If the upstream model list is non-empty and the agent's default model
//     appears in it, prefer the upstream model (direct use, no proxy
//     redirection needed).
//  2. Otherwise, fall back to the proxy model map. If the agent's default
//     model exists in the map, use the mapped target.
//  3. As a last resort, keep the agent's default model (the proxy will
//     attempt to forward it as-is).
//
// upstreamModels is the set of model IDs returned by the proxy's /v1/models
// endpoint. Pass nil or an empty slice to skip upstream lookup.
// proxyModelMap is the proxy's model mapping (e.g. {"gpt-5.5": "sensenova-..."}).
//
// The returned (model, source) indicates what model name should be written into
// the agent config and where that choice came from: "upstream", "proxy-map", or
// "default".
func ResolveModelForAgent(agentName, defaultModel string, upstreamModels []string, proxyModelMap map[string]string) (model string, source string) {
	upstreamSet := make(map[string]bool)
	for _, m := range upstreamModels {
		upstreamSet[m] = true
	}

	// 1. Prefer upstream model if the default model exists upstream
	if len(upstreamSet) > 0 && upstreamSet[defaultModel] {
		return defaultModel, "upstream"
	}

	// 2. Fall back to proxy model map
	if proxyModelMap != nil {
		if target, ok := proxyModelMap[defaultModel]; ok {
			return target, "proxy-map"
		}
	}

	// 3. Keep default model (proxy forwards as-is)
	return defaultModel, "default"
}

// ResolveAllModels computes model resolution for every agent in the routing table.
// Returns a slice of Resolution entries, sorted by agent name.
func ResolveAllModels(upstreamModels []string, proxyModelMap map[string]string) []Resolution {
	upstreamSet := make(map[string]bool)
	for _, m := range upstreamModels {
		upstreamSet[m] = true
	}

	// Build a minimal proxy to reuse BuildRoutingTable
	p := &proxy.Proxy{ModelMap: proxyModelMap}
	routing := BuildRoutingTable(p)
	seen := make(map[string]bool)
	var agents []string
	for _, r := range routing {
		if r.Agent == "CCX-proxy" {
			continue
		}
		if !seen[r.Agent] {
			seen[r.Agent] = true
			agents = append(agents, r.Agent)
		}
	}
	sort.Strings(agents)

	var resolutions []Resolution
	for _, name := range agents {
		defaultModel, ok := shared.GetDefaultModel(name)
		if !ok {
			continue
		}
		target := defaultModel
		source := "default"
		notes := ""

		if len(upstreamSet) > 0 && upstreamSet[defaultModel] {
			target = defaultModel
			source = "upstream"
			notes = "upstream 支持，直接使用"
		} else if proxyModelMap != nil {
			if mapped, ok := proxyModelMap[defaultModel]; ok {
				target = mapped
				source = "proxy-map"
				notes = "上游不支持，走代理重定向"
			} else {
				notes = "上游不支持，使用默认模型（需代理重定向）"
			}
		}

		resolutions = append(resolutions, Resolution{
			Agent:   name,
			Model:   target,
			Default: defaultModel,
			Source:  source,
			Notes:   notes,
		})
	}

	return resolutions
}

// Resolution is one agent's model resolution result.
type Resolution struct {
	Agent   string
	Model   string
	Default string
	Source  string
	Notes   string
}

// NeedRedirect returns true if the model will require proxy redirection.
func (r *Resolution) NeedRedirect() bool {
	return r.Source != "upstream"
}

// String returns a one-line summary of the resolution.
func (r *Resolution) String() string {
	if r.NeedRedirect() {
		return r.Agent + ": " + r.Default + " -> " + r.Model + " [" + r.Source + "]"
	}
	return r.Agent + ": " + r.Model + " [upstream]"
}

// PickCustomModel selects the best upstream model ID for a custom-model agent.
// When "add all" mode is used, this function is called per-agent to pick the
// single model that best matches the agent's default model name, falling back
// to the first upstream model if there's no match.
//
// Used when conf set adds custom models: each agent gets the upstream model
// that best matches its default.
func PickCustomModel(agentName string, upstreamModels []string) string {
	defaultModel, ok := shared.GetDefaultModel(agentName)
	if !ok || len(upstreamModels) == 0 {
		return ""
	}

	// Exact match is best.
	for _, m := range upstreamModels {
		if strings.EqualFold(m, defaultModel) {
			return m
		}
	}

	// Keyword match: use the agent's default model name as the keyword.
	for _, m := range upstreamModels {
		if strings.Contains(strings.ToLower(m), strings.ToLower(defaultModel)) {
			return m
		}
	}

	// Fallback: first upstream model.
	return upstreamModels[0]
}

// GetAllUpstreamModels returns the full deduplicated list of upstream model IDs
// stored in a DB proxy record, ready to be written as custom models.
func GetAllUpstreamModels(r *db.ProxyRecord) []string {
	return db.GetModelsFromRecord(r)
}
