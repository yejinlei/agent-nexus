package agent

import (
	"os"
	"strings"
	"agent-nexus/internal/proxy"
)

type hermesWriter struct{}

func newHermesWriter() *hermesWriter { return &hermesWriter{} }

func (w *hermesWriter) Name() string     { return "hermes" }
func (w *hermesWriter) Category() string { return "cli" }
func (w *hermesWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

func (w *hermesWriter) Configure(path string, p *proxy.Proxy) error {
	content := "# Hermes Configuration - CCX Proxy\n" +
		"# Hermes uses ACP protocol with mcpServers for provider configuration\n\n" +
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

func (w *hermesWriter) Status(path string) (bool, string) {
	data, _ := os.ReadFile(path)
	s := string(data)
	return strings.Contains(s, "127.0.0.1") && strings.Contains(s, "3688"), "via CCX proxy"
}
