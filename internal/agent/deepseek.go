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

func (w *deepSeekWriter) Configure(path string, p *proxy.Proxy) error {
    content := "# DeepSeek TUI Configuration - AI Proxy\n# Or set DEEPSEEK_API_KEY environment variable\n\napi_key = \"" + p.APIKey + "\"\nbase_url = \"" + p.BaseURL + "\"\ndefault_text_model = \"sensenova-6.7-flash-lite\"\nreasoning_effort = \"high\"\n"
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
