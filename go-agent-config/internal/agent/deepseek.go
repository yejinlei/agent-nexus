package agent

import (
	"os"
	"strings"
	"go-agent-config/internal/proxy"
)

type deepSeekWriter struct{}

func newDeepSeekWriter() *deepSeekWriter { return &deepSeekWriter{} }

func (w *deepSeekWriter) Name() string     { return "deepseek" }
func (w *deepSeekWriter) Category() string { return "cli" }
func (w *deepSeekWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

func (w *deepSeekWriter) Configure(path string, p *proxy.Proxy) error {
	content := "# DeepSeek TUI Configuration - CCX Proxy\n# Or set DEEPSEEK_API_KEY environment variable\n\napi_key = \"" + p.APIKey + "\"\nbase_url = \"" + p.BaseURL + "\"\ndefault_text_model = \"sensenova-6.7-flash-lite\"\nreasoning_effort = \"high\"\n"
	return os.WriteFile(path, []byte(content), 0644)
}

func (w *deepSeekWriter) Status(path string) (bool, string) {
	data, _ := os.ReadFile(path)
	s := string(data)
	return strings.Contains(s, "127.0.0.1") && strings.Contains(s, "3688"), "via CCX proxy"
}
