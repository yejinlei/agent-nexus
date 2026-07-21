package agent

import (
	"encoding/json"
	"os"
	"strings"
	"agent-nexus/internal/proxy"
)

type openCodeWriter struct{}

func newOpenCodeWriter() *openCodeWriter { return &openCodeWriter{} }

func (w *openCodeWriter) Name() string     { return "opencode" }
func (w *openCodeWriter) Category() string { return "cli" }
func (w *openCodeWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

func (w *openCodeWriter) Configure(path string, p *proxy.Proxy, model string) error {
	if model == "" {
		model = "myccx/glm-5.2"
	}
	smallModel := "myccx/deepseek-v4-flash"

	var cfg map[string]interface{}
	data, err := os.ReadFile(path)
	if err != nil {
		cfg = make(map[string]interface{})
	} else if err := json.Unmarshal(data, &cfg); err != nil {
		cfg = make(map[string]interface{})
	}

	provider := map[string]interface{}{
		"$schema":     "https://opencode.ai/config.json",
		"model":       model,
		"small_model": smallModel,
	}

	provMap := map[string]interface{}{
		"myccx": map[string]interface{}{
			"npm": "@ai-sdk/openai-compatible",
			"name": "myccx",
			"options": map[string]interface{}{
				"baseURL": p.BaseURL,
				"apiKey":  p.APIKey,
			},
			"models": map[string]interface{}{
				"glm-5.2": map[string]interface{}{"name": "glm-5.2"},
			},
		},
	}
	provider["provider"] = provMap

	cfg["model"] = model
	cfg["small_model"] = smallModel
	cfg["provider"] = provMap

	out, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(path, out, 0644)
}

func (w *openCodeWriter) Status(path string) (bool, string) {
	data, _ := os.ReadFile(path)
	s := string(data)
	return strings.Contains(s, "127.0.0.1") && strings.Contains(s, "3688"), "via CCX proxy"
}

func (w *openCodeWriter) StatusModel(path string) (model, source, notes string) {
	_, source, notes = defaultModelInfo(w.Name())
	model, found := extractModelFromConfig(path)
	if found {
		return model, source, notes
	}
	return "", source, notes
}
