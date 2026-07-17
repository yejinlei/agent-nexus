package agent

import (
	"os"
	"strings"
	"go-agent-config/internal/proxy"
)

type codexWriter struct{}

func newCodexWriter() *codexWriter { return &codexWriter{} }

func (w *codexWriter) Name() string     { return "codex" }
func (w *codexWriter) Category() string { return "cli" }
func (w *codexWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

func (w *codexWriter) Configure(path string, p *proxy.Proxy) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content := string(data)

	// Replace or add openai_base_url
	content = applyPattern(content, "openai_base_url\\s*=\\s*\".*\"", "openai_base_url = \""+p.BaseURL+"\"")
	content = applyPattern(content, "model_provider\\s*=\\s*\".*\"", "model_provider = \"openai\"")
	content = applyPattern(content, "model\\s*=\\s*\".*\"", "model = \"gpt-5.5\"")

	// Add ccswitch provider block if missing
	if !strings.Contains(content, "[model_providers.ccswitch]") {
		content += "\n[model_providers.ccswitch]\nname = \"Sensenova\"\nbase_url = \"https://token.sensenova.cn/v1\"\nrequires_openai_auth = false\n"
	}

	// Add API key
	if !strings.Contains(content, "api_key") {
		content += "\napi_key = \"" + p.APIKey + "\"\n"
	}

	return os.WriteFile(path, []byte(content), 0644)
}

func (w *codexWriter) Status(path string) (bool, string) {
	data, _ := os.ReadFile(path)
	s := string(data)
	return strings.Contains(s, "127.0.0.1") && strings.Contains(s, "3688"), "via CCX proxy"
}

func applyPattern(content, pattern, replacement string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if strings.Contains(line, pattern) {
			lines[i] = replacement
		}
	}
	return strings.Join(lines, "\n")
}
