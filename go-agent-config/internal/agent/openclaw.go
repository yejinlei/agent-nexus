package agent

import (
	"encoding/json"
	"os"
	"strings"
	"go-agent-config/internal/proxy"
)

type openClawWriter struct{}

func newOpenClawWriter() *openClawWriter { return &openClawWriter{} }

func (w *openClawWriter) Name() string     { return "openclaw" }
func (w *openClawWriter) Category() string { return "cli" }
func (w *openClawWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

func (w *openClawWriter) Configure(path string, p *proxy.Proxy) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}

	models := cfg["models"].(map[string]interface{})
	providers := models["providers"].(map[string]interface{})

	if _, ok := providers["sensenova-ccx"]; !ok {
		providers["sensenova-ccx"] = map[string]interface{}{
			"id":      "sensenova-ccx",
			"name":    "CCX-Sensenova",
			"baseUrl": "https://token.sensenova.cn/v1",
			"api":     "openai-completions",
			"models": []map[string]interface{}{
				{"id": "sensenova-6.7-flash-lite", "name": "Sensenova 6.7 Flash"},
				{"id": "deepseek-v4-flash",         "name": "DeepSeek V4 Flash"},
				{"id": "glm-5.2",                   "name": "GLM-5.2"},
				{"id": "sensenova-u1-fast",         "name": "Sensenova U1 Fast"},
			},
		}
	}

	out, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(path, out, 0644)
}

func (w *openClawWriter) Status(path string) (bool, string) {
	data, _ := os.ReadFile(path)
	s := string(data)
	return strings.Contains(s, "sensenova-ccx"), "sensenova-ccx provider configured"
}
