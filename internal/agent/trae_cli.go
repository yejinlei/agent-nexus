package agent

import (
	"os"
	"strings"
	"agent-nexus/internal/proxy"
)

type traeWriter struct{}

func newTraeWriter() *traeWriter { return &traeWriter{} }

func (w *traeWriter) Name() string     { return "trae" }
func (w *traeWriter) Category() string { return "cli" }
func (w *traeWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

func (w *traeWriter) Configure(path string, p *proxy.Proxy) error {
	content := "# Trae CLI Configuration - AI Proxy\n" +
		"# Trae uses ACP protocol with mcpServers for provider configuration\n\n" +
		"providers:\n" +
		"  ai-proxy:\n" +
		"    type: openai_legacy\n" +
		"    base_url: \"" + p.BaseURL + "\"\n" +
		"    api_key: \"" + p.APIKey + "\"\n" +
		"    models:\n" +
		"      default: sensenova-6.7-flash-lite\n\n" +
		"mcpServers:\n" +
		"  ai-proxy:\n" +
		"    type: http\n" +
		"    url: \"" + p.BaseURL + "\"\n" +
		"    apiKey: \"" + p.APIKey + "\"\n\n" +
		"default_model: sensenova-6.7-flash-lite\n"
	return os.WriteFile(path, []byte(content), 0644)
}

func (w *traeWriter) Status(path string) (bool, string) {
	data, _ := os.ReadFile(path)
	s := string(data)
	configured := strings.Contains(s, "127.0.0.1") ||
		strings.Contains(s, "platform.sensenova") || strings.Contains(s, "api.deepseek") ||
		strings.Contains(s, "api.siliconflow") || strings.Contains(s, "localhost:11434")
	if configured {
		return true, "via AI proxy"
	}
	return false, "未配置代理"
}
