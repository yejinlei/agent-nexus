package agent

import (
	"encoding/json"
	"os"
	"strings"
	"go-agent-config/internal/proxy"
)

type cursorWriter struct{}

func newCursorWriter() *cursorWriter { return &cursorWriter{} }

func (w *cursorWriter) Name() string     { return "cursor" }
func (w *cursorWriter) Category() string { return "ide" }
func (w *cursorWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

func (w *cursorWriter) Configure(path string, p *proxy.Proxy) error {
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
	cfg["cursor.ai.chat.model"]    = "sensenova-6.7-flash-lite"

	out, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(path, out, 0644)
}

func (w *cursorWriter) Status(path string) (bool, string) {
	data, _ := os.ReadFile(path)
	s := string(data)
	return strings.Contains(s, "cursor.ai.chat.apiBase") && strings.Contains(s, "127.0.0.1"), "OpenAI-compatible via CCX"
}
