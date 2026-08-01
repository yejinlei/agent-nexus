package agent

import (
	"os"
	"path/filepath"
	"strings"
	"agent-nexus/internal/proxy"
)

type openclaudeWriter struct{}

func newOpenClaudeWriter() *openclaudeWriter { return &openclaudeWriter{} }

func (w *openclaudeWriter) Name() string     { return "openclaude" }
func (w *openclaudeWriter) Category() string { return "cli" }
func (w *openclaudeWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

func (w *openclaudeWriter) Configure(path string, p *proxy.Proxy, model string) error {
	if model == "" { model = modelDefault(w.Name()) }
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	envFile := filepath.Join(home, ".openclaude-env")

	envContent := "# OpenClaude provider configuration (written by agent-nexus)`n"
	envContent += "CLAUDE_CODE_USE_OPENAI=1`n"
	envContent += "OPENAI_API_KEY=" + p.APIKey + "`n"
	envContent += "OPENAI_BASE_URL=" + p.BaseURL + "`n"
	envContent += "OPENAI_MODEL=" + model + "`n"

	return os.WriteFile(envFile, []byte(envContent), 0644)
}

func (w *openclaudeWriter) Status(path string) (bool, string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, "error: " + err.Error()
	}
	envFile := filepath.Join(home, ".openclaude-env")
	data, err := os.ReadFile(envFile)
	if err != nil {
		return false, "未配置代理"
	}
	s := string(data)
	configured := s != "" && (
		strings.Contains(s, "127.0.0.1") ||
		strings.Contains(s, "3688") ||
		strings.Contains(s, "sensenova") ||
		strings.Contains(s, "platform.sensenova") ||
		strings.Contains(s, "api.deepseek") ||
		strings.Contains(s, "api.siliconflow") ||
		strings.Contains(s, "localhost:11434"))
	if configured {
		return true, "via AI proxy"
	}
	return false, "未配置代理"
}

func (w *openclaudeWriter) StatusModel(path string) (model, source, notes string) {
	_, source, notes = defaultModelInfo(w.Name())
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "error", err.Error()
	}
	envFile := filepath.Join(home, ".openclaude-env")
	data, err := os.ReadFile(envFile)
	if err != nil {
		return "", "error", "配置文件未找到"
	}
	s := string(data)
	parts := strings.Split(s, "OPENAI_MODEL=")
	if len(parts) > 1 {
		modelParts := strings.Split(parts[1], "`n")
		return strings.TrimSpace(modelParts[0]), source, notes
	}
	return "", source, notes
}

// modelDefault returns the canonical default model for this writer's agent
// from the central shared.DefaultModels map.
