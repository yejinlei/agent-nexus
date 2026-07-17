package agent

import (
	"os"
	"strings"
	"agent-nexus/internal/proxy"
)

type grokWriter struct{}

func newGrokWriter() *grokWriter { return &grokWriter{} }

func (w *grokWriter) Name() string     { return "grok" }
func (w *grokWriter) Category() string { return "cli" }
func (w *grokWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

func (w *grokWriter) Configure(path string, p *proxy.Proxy) error {
	content := "# Grok Build CLI Configuration - CCX Proxy\n" +
		"# Grok uses ACP protocol with mcpServers for provider configuration\n\n" +
		"providers:\n" +
		"  ccx:\n" +
		"    type: openai\n" +
		"    base_url: \"" + p.BaseURL + "\"\n" +
		"    api_key: \"" + p.APIKey + "\"\n" +
		"    models:\n" +
		"      default: sensenova-6.7-flash-lite\n\n" +
		"mcpServers:\n" +
		"  ccx:\n" +
		"    type: http\n" +
		"    url: \"" + p.BaseURL + "\"\n" +
		"    apiKey: \"" + p.APIKey + "\"\n\n" +
		"default_model: sensenova-6.7-flash-lite\n"

	return os.WriteFile(path, []byte(content), 0644)
}

func (w *grokWriter) Status(path string) (bool, string) {
	data, _ := os.ReadFile(path)
	s := string(data)
	return strings.Contains(s, "127.0.0.1") && strings.Contains(s, "3688"), "via CCX proxy"
}
