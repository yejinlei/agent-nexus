package agent

import (
	"os"
	"strings"
	"agent-nexus/internal/proxy"
)

type deepSeekWriter struct{}

func newDeepSeekWriter() *deepSeekWriter { return &deepSeekWriter{} }

func (w *deepSeekWriter) Name() string     { return "deepseek" }
func (w *deepSeekWriter) Category() string { return "cli" }
func (w *deepSeekWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

func (w *deepSeekWriter) Configure(path string, p *proxy.Proxy, model string) error {
	if model == "" {
		model = "sensenova-6.7-flash-lite"
	}
	content := "# DeepSeek TUI Configuration - AI Proxy\n" +
		"# Or set DEEPSEEK_API_KEY environment variable\n\n" +
		"api_key = \"" + p.APIKey + "\"\n" +
		"base_url = \"" + p.BaseURL + "\"\n" +
		"default_text_model = \"" + model + "\"\n" +
		"reasoning_effort = \"high\"\n"
	return os.WriteFile(path, []byte(content), 0644)
}

func (w *deepSeekWriter) Status(path string) (bool, string) {
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

func (w *deepSeekWriter) StatusModel(path string) (model, source, notes string) {
	_, source, notes = defaultModelInfo(w.Name())
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "error", "配置文件未找到"
	}
	s := string(data)
	// Look for default_text_model = "xxx"
	if idx := strings.Index(s, "default_text_model = \""); idx >= 0 {
		end := strings.Index(s[idx+len("default_text_model = \""):], "\"")
		if end >= 0 {
			return s[idx+len("default_text_model = \""):idx+len("default_text_model = \"")+end], source, notes
		}
	}
	return "", source, notes
}
