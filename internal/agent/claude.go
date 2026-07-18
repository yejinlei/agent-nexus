package agent

import (
    "encoding/json"
    "os"
    "strings"
    "agent-nexus/internal/proxy"
)

type claudeWriter struct{}

func newClaudeWriter() *claudeWriter { return &claudeWriter{} }

func (w *claudeWriter) Name() string     { return "claude" }
func (w *claudeWriter) Category() string { return "cli" }
func (w *claudeWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

func (w *claudeWriter) Configure(path string, p *proxy.Proxy) error {
    var cfg map[string]interface{}
    data, err := os.ReadFile(path)
    if err != nil {
        return err
    }
    if err := json.Unmarshal(data, &cfg); err != nil {
        return err
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

func (w *claudeWriter) Status(path string) (bool, string) {
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
