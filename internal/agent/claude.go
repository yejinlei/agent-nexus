package agent

import (
	"agent-nexus/internal/proxy"
	"encoding/json"
	"fmt"
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
	return w.ConfigureTiered(path, p, map[string]string{"default": model})
}

// ConfigureTiered writes Claude's settings.json with per-tier model mappings.
// tiers["opus"] / ["sonnet"] / ["haiku"] are the named roles; each falls back
// to tiers["default"] when empty.
func (w *claudeWriter) ConfigureTiered(path string, p *proxy.Proxy, tiers map[string]string) error {
	defaultModel := tiers["default"]
	if defaultModel == "" {
		defaultModel = modelDefault(w.Name())
		if defaultModel == "" {
			return fmt.Errorf("未找到 %s 的默认模型", w.Name())
		}
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

	// Per-tier model mappings: Claude CLI uses these to translate its
	// internal "opus / sonnet / haiku" tiers to the real upstream model
	// name. Without them, a tier switch sends an unknown model name to the
	// proxy and 404s.
	for _, tier := range []string{"OPUS", "SONNET", "HAIKU"} {
		key := strings.ToLower(tier)
		tierModel := tiers[key]
		if tierModel == "" {
			tierModel = defaultModel
		}
		env["ANTHROPIC_DEFAULT_"+tier+"_MODEL"]     = tierModel
		env["ANTHROPIC_DEFAULT_"+tier+"_MODEL_NAME"] = tierModel
	}

	cfg["env"] = env
	cfg["model"] = defaultModel
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

// keysWritersOwn lists the top-level keys in settings.json that
// agent-nexus sets. Reset removes them so Claude falls back to its defaults.
var keysWritersOwn = []string{"env", "model", "effortLevel"}

// Reset surgically removes agent-nexus injected keys from Claude's settings.json.
func (w *claudeWriter) Reset(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	for _, k := range keysWritersOwn {
		delete(cfg, k)
	}

	// If nothing is left, delete the file entirely.
	if len(cfg) == 0 {
		_ = os.Remove(path)
		return []string{}, nil
	}

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, err
	}
	return nil, os.WriteFile(path, append(out, '\n'), 0644)
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
