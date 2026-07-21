package agent

import (
	"encoding/json"
	"os"
	"strings"
	"agent-nexus/internal/proxy"
)

type openClawWriter struct{}

func newOpenClawWriter() *openClawWriter { return &openClawWriter{} }

func (w *openClawWriter) Name() string     { return "openclaw" }
func (w *openClawWriter) Category() string { return "cli" }
func (w *openClawWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

func (w *openClawWriter) Configure(path string, p *proxy.Proxy, model string) error {
	if model == "" {
		model = "sensenova-6.7-flash-lite"
	}
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
				{"id": model, "name": "Sensenova 6.7 Flash"},
				{"id": "deepseek-v4-flash", "name": "DeepSeek V4 Flash"},
				{"id": "glm-5.2", "name": "GLM-5.2"},
				{"id": "sensenova-u1-fast", "name": "Sensenova U1 Fast"},
			},
		}
	}

	out, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(path, out, 0644)
}

func (w *openClawWriter) Status(path string) (bool, string) {
	data, _ := os.ReadFile(path)
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
	// Look for the first model id in the providers block
	if idx := strings.Index(s, "\"id\": \""); idx >= 0 {
		end := strings.Index(s[idx+len("\"id\": \""):], "\"")
		if end >= 0 {
			return s[idx+len("\"id\": \""):idx+len("\"id\": \"")+end], source, notes
		}
	}
	return "", source, notes
}
