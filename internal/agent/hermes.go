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

func (w *hermesWriter) Configure(path string, p *proxy.Proxy, model string) error {
	if model == "" {
		model = "sensenova-6.7-flash-lite"
	}
	content := "# Hermes Configuration - CCX Proxy\n" +
		"# Hermes Configuration - CCX Proxy uses ACP protocol with mcpServers for provider configuration\n\n" +
		"providers:\n" +
		"  ccx:\n" +
		"    type: openai_legacy\n" +
		"    base_url: \"" + p.BaseURL + "\"\n" +
		"    api_key: \"" + p.APIKey + "\"\n" +
		"    models:\n" +
		"      default: " + model + "\n\n" +
		"mcpServers:\n" +
		"  ccx:\n" +
		"    type: http\n" +
		"    url: \"" + p.BaseURL + "\"\n" +
		"    apiKey: \"" + p.APIKey + "\"\n\n" +
		"default_model: " + model + "\n"
	return os.WriteFile(path, []byte(content), 0644)
}

func (w *hermesWriter) Status(path string) (bool, string) {
	data, _ := os.ReadFile(path)
	s := string(data)
	if strings.Contains(s, "127.0.0.1") ||
		strings.Contains(s, "platform.sensenova") || strings.Contains(s, "api.deepseek") ||
		strings.Contains(s, "api.siliconflow") || strings.Contains(s, "localhost:11434") {
		return true, "via AI proxy"
	}
	return false, "未配置代理"
}

func (w *hermesWriter) StatusModel(path string) (model, source, notes string) {
	_, source, notes = defaultModelInfo(w.Name())
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "error", "配置文件未找到"
	}
	s := string(data)
	if idx := strings.Index(s, "default_model: "); idx >= 0 {
		line := s[idx+len("default_model: "):]
		if i := strings.Index(line, "\n"); i >= 0 {
			return strings.TrimSpace(line[:i]), source, notes
		}
		return strings.TrimSpace(line), source, notes
	}
	return "", source, notes
}
