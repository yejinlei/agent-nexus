package agent

import (
	"agent-nexus/internal/proxy"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type claudeWriter struct{}

func newClaudeWriter() *claudeWriter { return &claudeWriter{} }

func (w *claudeWriter) Name() string                     { return "claude" }
func (w *claudeWriter) Category() string                 { return "cli" }
func (w *claudeWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

func (w *claudeWriter) Configure(path string, p *proxy.Proxy, model string) error {
	if model == "" {
		model = modelDefault(w.Name())
	}

	cfg := make(map[string]interface{})
	data, err := os.ReadFile(path)
	if err != nil {
		// New file: start with a clean Claude desktop config structure.
		cfg = make(map[string]interface{})
	} else if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}

	env := make(map[string]interface{})
	if e, ok := cfg["env"]; ok {
		env = e.(map[string]interface{})
	}
	env["ANTHROPIC_BASE_URL"] = strings.TrimSuffix(p.BaseURL, "/v1")
	env["ANTHROPIC_AUTH_TOKEN"] = p.APIKey
	cfg["env"] = env
	cfg["model"] = model
	cfg["effortLevel"] = "high"

	out, _ := json.MarshalIndent(cfg, "", "  ")

	// Ensure the parent directory exists (e.g. ~/.claude/ may not exist).
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(path, out, 0644)
}

func (w *claudeWriter) Status(path string) (bool, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, "未配置代理"
	}
	s := string(data)
	configured := strings.Contains(s, "127.0.0.1") ||
		strings.Contains(s, "platform.sensenova") || strings.Contains(s, "api.deepseek") ||
		strings.Contains(s, "api.siliconflow") || strings.Contains(s, "localhost:11434")
	if configured {
		return true, "via AI proxy"
	}
	return false, "未配置代理"
}

func (w *claudeWriter) StatusModel(path string) (model, source, notes string) {
	_, source, notes = defaultModelInfo(w.Name())
	modelName, found := extractModelFromConfig(path)
	if found {
		return modelName, source, notes
	}
	return "", source, notes
}

// modelDefault returns the canonical default model for this writer's agent
// from the central shared.DefaultModels map.
