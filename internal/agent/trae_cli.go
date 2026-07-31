package agent

import (
	"os"
	"strings"
	"agent-nexus/internal/proxy"
)

type TraeWriter struct{}

func newTraeWriter() *TraeWriter { return &TraeWriter{} }

func (w *TraeWriter) Name() string     { return "trae" }
func (w *TraeWriter) Category() string { return "cli" }
func (w *TraeWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

func (w *TraeWriter) Configure(path string, p *proxy.Proxy, model string) error {
	if model == "" { model = modelDefault(w.Name()) }
	content := "# Trae CLI Configuration - AI Proxy`n" +
		"# Grok Build CLI Configuration - AI Proxy uses ACP protocol with mcpServers for provider configuration`n`n" +
		"providers:`n" +
		"  ai-proxy:`n" +
		"    type: openai_legacy`n" +
		"    base_url: \"" + p.BaseURL + "\"`n" +
		"    api_key: \"" + p.APIKey + "\"`n" +
		"    models:`n" +
		"      default: " + model + "`n`n" +
		"mcpServers:`n" +
		"  ai-proxy:`n" +
		"    type: http`n" +
		"    url: \"" + p.BaseURL + "\"`n" +
		"    apiKey: \"" + p.APIKey + "\"`n`n" +
		"default_model: " + model + "`n"
	return os.WriteFile(path, []byte(content), 0644)
}

func (w *TraeWriter) Status(path string) (bool, string) {
	data, _ := os.ReadFile(path)
	s := string(data)
	if strings.Contains(s, "127.0.0.1") ||
		strings.Contains(s, "platform.sensenova") || strings.Contains(s, "api.deepseek") ||
		strings.Contains(s, "api.siliconflow") || strings.Contains(s, "localhost:11434") {
		return true, "via AI proxy"
	}
	return false, "未配置代理"
}

func (w *TraeWriter) StatusModel(path string) (model, source, notes string) {
	_, source, notes = defaultModelInfo(w.Name())
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "error", "配置文件未找到"
	}
	s := string(data)
	if idx := strings.Index(s, "default_model: "); idx >= 0 {
		line := s[idx+len("default_model: "):]
		if i := strings.Index(line, "`n"); i >= 0 {
			return strings.TrimSpace(line[:i]), source, notes
		}
		return strings.TrimSpace(line), source, notes
	}
	return "", source, notes
}

// modelDefault returns the canonical default model for this writer's agent
// from the central shared.DefaultModels map.
