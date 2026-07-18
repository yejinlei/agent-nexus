package agent

import (
    "encoding/json"
    "os"
    "strings"
    "agent-nexus/internal/proxy"
)

type codeBuddyWriter struct{}

func newCodeBuddyWriter() *codeBuddyWriter { return &codeBuddyWriter{} }

func (w *codeBuddyWriter) Name() string     { return "codebuddy" }
func (w *codeBuddyWriter) Category() string { return "cli" }
func (w *codeBuddyWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

func (w *codeBuddyWriter) Configure(path string, p *proxy.Proxy) error {
    var cfg map[string]interface{}
    data, err := os.ReadFile(path)
    if err != nil {
        cfg = make(map[string]interface{})
    } else if err := json.Unmarshal(data, &cfg); err != nil {
        cfg = make(map[string]interface{})
    }

    env := make(map[string]interface{})
    if e, ok := cfg["env"]; ok {
        env = e.(map[string]interface{})
    }
    env["ANTHROPIC_BASE_URL"] = strings.TrimSuffix(p.BaseURL, "/v1")
    env["ANTHROPIC_AUTH_TOKEN"] = p.APIKey
    cfg["env"] = env
    cfg["model"] = "fable"
    cfg["effortLevel"] = "high"

    out, _ := json.MarshalIndent(cfg, "", "  ")
    return os.WriteFile(path, out, 0644)
}

func (w *codeBuddyWriter) Status(path string) (bool, string) {
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
