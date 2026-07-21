package agent

import (
	"encoding/json"
	"os"
	"strings"
	"agent-nexus/internal/proxy"
)

type cursorWriter struct{}

func newCursorWriter() *cursorWriter { return &cursorWriter{} }

func (w *cursorWriter) Name() string     { return "cursor" }
func (w *cursorWriter) Category() string { return "ide" }
func (w *cursorWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

func (w *cursorWriter) Configure(path string, p *proxy.Proxy, model string) error {
	if model == "" {
		model = "sensenova-6.7-flash-lite"
	}
	var cfg map[string]interface{}
	data, err := os.ReadFile(path)
	if err != nil {
		cfg = make(map[string]interface{})
	} else if err := json.Unmarshal(data, &cfg); err != nil {
		cfg = make(map[string]interface{})
	}

	cfg["cursor.ai.chat.provider"] = "openai-compatible"
	cfg["cursor.ai.chat.apiBase"]  = p.BaseURL
	cfg["cursor.ai.chat.apiKey"]   = p.APIKey
	cfg["cursor.ai.chat.model"]    = model

	out, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(path, out, 0644)
}

func (w *cursorWriter) Status(path string) (bool, string) {
	data, _ := os.ReadFile(path)
	s := string(data)
	configured := strings.Contains(s, "127.0.0.1") ||
		strings.Contains(s, "platform.sensenova") || strings.Contains(s, "api.deepseek") ||
		strings.Contains(s, "api.siliconflow") || strings.Contains(s, "localhost:11434")
	if configured {
		return true, "OpenAI-compatible via AI proxy"
	}
	return false, "未配置代理"
}

func (w *cursorWriter) StatusModel(path string) (model, source, notes string) {
	_, source, notes = defaultModelInfo(w.Name())
	model, found := extractModelFromConfig(path)
	if found {
		return model, source, notes
	}
	return "", source, notes
}
