package agent

import (
	"agent-nexus/internal/proxy"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type openClawWriter struct{}

func newOpenClawWriter() *openClawWriter { return &openClawWriter{} }

func (w *openClawWriter) Name() string                     { return "openclaw" }
func (w *openClawWriter) Category() string                 { return "cli" }
func (w *openClawWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

func (w *openClawWriter) Configure(path string, p *proxy.Proxy, model string) error {
	if model == "" {
		model = modelDefault(w.Name())
	}

	// Read existing config, preserving non-model fields (channels, memory, etc.)
	var cfg map[string]interface{}
	data, err := os.ReadFile(path)
	if err == nil {
		if parseErr := json.Unmarshal(data, &cfg); parseErr != nil {
			cfg = make(map[string]interface{})
		}
	} else {
		cfg = make(map[string]interface{})
	}

	// --- models.providers ---
	models, ok := cfg["models"].(map[string]interface{})
	if !ok {
		models = map[string]interface{}{"mode": "merge"}
		cfg["models"] = models
	}
	providers, ok := models["providers"].(map[string]interface{})
	if !ok {
		providers = map[string]interface{}{}
		models["providers"] = providers
	}

	providers["sensenova-ccx"] = map[string]interface{}{
		"baseUrl": p.BaseURL,
		"apiKey":  p.APIKey,
		"api":     "openai-completions",
		"models": []map[string]interface{}{
			{"id": model, "name": "Sensenova " + model, "reasoning": false, "input": []interface{}{"text"}, "contextWindow": 200000, "maxTokens": 4096, "cost": map[string]interface{}{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0}},
			{"id": "deepseek-v4-flash", "name": "DeepSeek V4 Flash", "reasoning": false, "input": []interface{}{"text"}, "contextWindow": 200000, "maxTokens": 4096, "cost": map[string]interface{}{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0}},
			{"id": "glm-5.2", "name": "GLM-5.2", "reasoning": false, "input": []interface{}{"text"}, "contextWindow": 200000, "maxTokens": 4096, "cost": map[string]interface{}{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0}},
			{"id": "sensenova-u1-fast", "name": "Sensenova U1 Fast", "reasoning": false, "input": []interface{}{"text"}, "contextWindow": 200000, "maxTokens": 4096, "cost": map[string]interface{}{"input": 0, "output": 0, "cacheRead": 0, "cacheWrite": 0}},
		},
	}

	// --- agents.defaults.model.primary (required so agent starts with the right model) ---
	agentRef := "sensenova-ccx/" + model
	agents, ok := cfg["agents"].(map[string]interface{})
	if !ok {
		agents = map[string]interface{}{}
		cfg["agents"] = agents
	}
	defaults, ok := agents["defaults"].(map[string]interface{})
	if !ok {
		defaults = map[string]interface{}{}
		agents["defaults"] = defaults
	}
	defaults["model"] = map[string]interface{}{
		"primary": agentRef,
	}
	// Register the model in the catalog so /model shows it
	modelsCatalog, ok := defaults["models"].(map[string]interface{})
	if !ok {
		modelsCatalog = map[string]interface{}{}
		defaults["models"] = modelsCatalog
	}
	modelsCatalog[agentRef] = map[string]interface{}{"alias": model}

	// --- env block for any env-based tooling ---
	cfg["env"] = map[string]interface{}{
		"OPENAI_API_KEY":  p.APIKey,
		"OPENAI_BASE_URL": p.BaseURL,
	}

	out, marshalErr := json.MarshalIndent(cfg, "", "  ")
	if marshalErr != nil {
		return marshalErr
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(path, out, 0644)
}

func (w *openClawWriter) Status(path string) (bool, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, "未配置代理"
	}
	s := string(data)
	configured := strings.Contains(s, "sensenova-ccx") ||
		strings.Contains(s, "127.0.0.1") ||
		strings.Contains(s, "platform.sensenova") || strings.Contains(s, "api.deepseek") ||
		strings.Contains(s, "api.siliconflow") || strings.Contains(s, "localhost:11434")
	if configured {
		return true, "sensenova-ccx provider configured"
	}
	return false, "未配置代理"
}

func (w *openClawWriter) StatusModel(path string) (model, source, notes string) {
	_, source, notes = defaultModelInfo(w.Name())
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "error", "配置文件未找到"
	}
	s := string(data)
	if idx := strings.Index(s, "\"id\": \""); idx >= 0 {
		end := strings.Index(s[idx+len("\"id\": \""):], "\"")
		if end >= 0 {
			return s[idx+len("\"id\": \"") : idx+len("\"id\": \"")+end], source, notes
		}
	}
	return "", source, notes
}

// Reset surgically removes agent-nexus injected provider, model, and env keys.
func (w *openClawWriter) Reset(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// Remove the provider we injected.
	if models, ok := cfg["models"].(map[string]interface{}); ok {
		if providers, ok := models["providers"].(map[string]interface{}); ok {
			delete(providers, "sensenova-ccx")
			// If models.mode was our default and no other providers remain, drop it.
			if len(providers) == 0 && models["mode"] == "merge" {
				delete(models, "providers")
				delete(models, "mode")
			}
		}
		if len(models) == 0 {
			delete(cfg, "models")
		}
	}

	// Remove agents.defaults.model (the provider ref we set).
	if agents, ok := cfg["agents"].(map[string]interface{}); ok {
		if defaults, ok := agents["defaults"].(map[string]interface{}); ok {
			delete(defaults, "model")
			// Remove our catalog entry for the provider model.
			if cat, ok := defaults["models"].(map[string]interface{}); ok {
				for k := range cat {
					if strings.Contains(k, "sensenova-ccx") {
						delete(cat, k)
					}
				}
				if len(cat) == 0 {
					delete(defaults, "models")
				}
			}
			if len(defaults) == 0 {
				delete(agents, "defaults")
			}
		}
		if len(agents) == 0 {
			delete(cfg, "agents")
		}
	}

	// Remove env block (we created it entirely).
	delete(cfg, "env")

	if len(cfg) == 0 {
		_ = os.Remove(path)
		return nil, nil
	}

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return nil, os.WriteFile(path, append(out, '\n'), 0644)
}

// modelDefault returns the canonical default model for this writer's agent
// from the central shared.DefaultModels map.
