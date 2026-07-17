package agent

import (
	"encoding/json"
	"os"
	"strings"
	"go-agent-config/internal/proxy"
)

type openCodeWriter struct{}

func newOpenCodeWriter() *openCodeWriter { return &openCodeWriter{} }

func (w *openCodeWriter) Name() string     { return "opencode" }
func (w *openCodeWriter) Category() string { return "cli" }
func (w *openCodeWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

func (w *openCodeWriter) Configure(path string, p *proxy.Proxy) error {
	var cfg map[string]interface{}
	data, err := os.ReadFile(path)
	if err != nil {
		cfg = make(map[string]interface{})
	} else if err := json.Unmarshal(data, &cfg); err != nil {
		cfg = make(map[string]interface{})
	}

	provider := map[string]interface{}{
		"$schema": "https://opencode.ai/config.json",
		"model":   "myccx/glm-5.2",
		"small_model": "myccx/deepseek-v4-flash",
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

	cfg["model"] = "myccx/glm-5.2"
	cfg["small_model"] = "myccx/deepseek-v4-flash"
	cfg["provider"] = provMap

	out, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(path, out, 0644)
}

func (w *openCodeWriter) Status(path string) (bool, string) {
	data, _ := os.ReadFile(path)
	s := string(data)
	return strings.Contains(s, "127.0.0.1") && strings.Contains(s, "3688"), "via CCX proxy"
}
