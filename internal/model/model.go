package model

import "agent-nexus/internal/proxy"

// ModelMapping represents a model routing entry
type ModelMapping struct {
	Agent  string
	Model  string
	Target string
	Source string
}

// BuildRoutingTable returns model routing info based on detected proxy settings
func BuildRoutingTable(p *proxy.Proxy) []ModelMapping {
	routing := []ModelMapping{
		{"codex", "gpt-5.5", "sensenova-6.7-flash-lite", "CCX"},
		{"claude", "fable", "glm-5.2", "CCX"},
		{"kimi", "ccx/gpt-5.5", "sensenova-6.7-flash-lite", "CCX"},
		{"deepseek", "sensenova-6.7-flash-lite", "sensenova-6.7-flash-lite", "CCX"},
		{"opencode", "myccx/glm-5.2", "glm-5.2", "CCX"},
		{"cursor", "sensenova-6.7-flash-lite", "sensenova-6.7-flash-lite", "CCX"},
	}

	if p.ModelMap != nil {
		for src, dst := range p.ModelMap {
			routing = append(routing, ModelMapping{Agent: "CCX-proxy", Model: src, Target: dst, Source: "CCX-modelmap"})
		}
	}

	return routing
}

// FindBestModel returns the best target model for a given agent based on routing table
func FindBestModel(agentName, proxyBaseModel string, table []ModelMapping) (string, string) {
	for _, m := range table {
		if m.Agent == agentName {
			return m.Model, m.Target
		}
	}
	if proxyBaseModel != "" {
		return proxyBaseModel, "via CCX proxy"
	}
	return "", ""
}


