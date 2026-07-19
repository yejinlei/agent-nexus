package model

import (
	"agent-nexus/internal/discover"
	"agent-nexus/internal/proxy"
)

// ModelMapping represents a model routing entry
type ModelMapping struct {
	Agent  string
	Model  string
	Target string
	Source string
}

// ModelDetail holds the model-info summary for a single discovered agent
type ModelDetail struct {
	AgentName      string
	Category       string
	IsConfigured   bool
	IsConfigurable bool
	DefaultModel   string // model name agent-nexus writes (e.g. "gpt-5.5")
	RoutedTo       string // backend model the proxy targets (e.g. "sensenova-6.7-flash-lite")
	SupportsCustom bool   // true if the agent can accept any OpenAI-compatible model name
	ModelSource    string // where the model comes from: "proxy-map" | "N/A"
	Notes          string // explanation about custom models vs. redefinition
}

// BuildRoutingTable returns model routing info based on detected proxy settings
// Supports both CCX Desktop (ccx/Desktop) and CC-Switch (ccx/Switch) proxies
func BuildRoutingTable(p *proxy.Proxy) []ModelMapping {
	routing := []ModelMapping{
		{"codex", "gpt-5.5", "sensenova-6.7-flash-lite", "CCX"},
		{"claude", "fable", "glm-5.2", "CCX"},
		{"kimi", "gpt-5.5", "sensenova-6.7-flash-lite", "CCX"},
		{"deepseek", "sensenova-6.7-flash-lite", "sensenova-6.7-flash-lite", "CCX"},
		{"opencode", "myccx/glm-5.2", "glm-5.2", "CCX"},
		{"cursor", "sensenova-6.7-flash-lite", "sensenova-6.7-flash-lite", "CCX"},
		{"openclaw", "sensenova-6.7-flash-lite", "sensenova-6.7-flash-lite", "CCX"},
		{"codebuddy", "fable", "glm-5.2", "CCX"},
		{"hermes", "sensenova-6.7-flash-lite", "sensenova-6.7-flash-lite", "CCX"},
		{"kiro", "sensenova-6.7-flash-lite", "sensenova-6.7-flash-lite", "CCX"},
		{"grok", "sensenova-6.7-flash-lite", "sensenova-6.7-flash-lite", "CCX"},
		{"qoder", "sensenova-6.7-flash-lite", "sensenova-6.7-flash-lite", "CCX"},
		{"trae", "sensenova-6.7-flash-lite", "sensenova-6.7-flash-lite", "CCX"},
	}

	if p != nil && p.ModelMap != nil {
		for src, dst := range p.ModelMap {
			routing = append(routing, ModelMapping{Agent: "CCX-proxy", Model: src, Target: dst, Source: string(p.Source)})
		}
	}

	return routing
}

// BuildModelDetails enriches each discovered agent with model-info fields.
// Pass the proxy (may be nil if not detected) and the agent list.
func BuildModelDetails(p *proxy.Proxy, agents []discover.AgentInfo) map[string]*ModelDetail {
	// Default model each agent uses when configured by agent-nexus
	defaultRouting := map[string]string{
		"codex":     "gpt-5.5",
		"claude":    "fable",
		"kimi":      "gpt-5.5",
		"deepseek":  "sensenova-6.7-flash-lite",
		"opencode":  "myccx/glm-5.2",
		"cursor":    "sensenova-6.7-flash-lite",
		"openclaw":  "sensenova-6.7-flash-lite",
		"codebuddy": "fable",
		"hermes":    "sensenova-6.7-flash-lite",
		"kiro":      "sensenova-6.7-flash-lite",
		"grok":      "sensenova-6.7-flash-lite",
		"qoder":     "sensenova-6.7-flash-lite",
		"trae":      "sensenova-6.7-flash-lite",
	}

	details := make(map[string]*ModelDetail)
	for _, a := range agents {
		md := &ModelDetail{
			AgentName:      a.Name,
			Category:       a.Category,
			IsConfigured:   a.IsConfigured,
			IsConfigurable: a.IsConfigurable,
			Notes:          a.Notes,
		}

		if !a.IsConfigurable {
			md.DefaultModel = "N/A"
			md.RoutedTo = "N/A"
			md.SupportsCustom = false
			md.ModelSource = "N/A"
			details[a.Name] = md
			continue
		}

		md.DefaultModel = defaultRouting[a.Name]

		// Find target from routing table
		routing := BuildRoutingTable(p)
		for _, r := range routing {
			if r.Agent == a.Name {
				md.RoutedTo = r.Target
				break
			}
		}

		// All configurable agents use an OpenAI-compatible provider.
		// They can accept any model name the backend supports; however the user
		// must use model redefinition (proxy model map) to map a custom name to
		// a real backend model.
		md.SupportsCustom = true
		md.ModelSource = "proxy-map"
		md.Notes = "OpenAI 兼容协议：可通过模型重定义（proxy model map）映射自定义模型名到后端"

		details[a.Name] = md
	}

	return details
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
