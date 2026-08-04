package agent

import (
	"agent-nexus/internal/proxy"
	"agent-nexus/internal/sniff"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type geminiWriter struct{}

func newGeminiWriter() *geminiWriter { return &geminiWriter{} }

func (w *geminiWriter) Name() string                     { return "gemini" }
func (w *geminiWriter) Category() string                 { return "cli" }
func (w *geminiWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

// Configure writes Gemini CLI settings (~/.gemini/settings.json) and
// provider env file (~/.gemini/.env). Gemini CLI requires the Google
// Gemini native protocol (/v1beta/...). We probe the endpoint before
// writing; if the probe fails we refuse with a clear warning.
func (w *geminiWriter) Configure(path string, p *proxy.Proxy, model string) error {
	if model == "" {
		model = modelDefault(w.Name())
	}

	home, _ := os.UserHomeDir()
	if home == "" {
		return os.WriteFile(path, []byte("home dir unavailable"), 0644)
	}

	geminiDir := filepath.Join(home, ".gemini")
	_ = os.MkdirAll(geminiDir, 0755)

	// --- Probe Gemini native protocol ---
	if !sniff.GeminiProtocolProbe(p.BaseURL, p.APIKey) {
		return &ErrProtocolIncompatible{
			Agent:    "gemini",
			BaseURL:  p.BaseURL,
			Reason:   "需要 Gemini 原生协议 (/v1beta/...)",
			Fallback: "使用支持 Gemini 原生协议的代理（如 CCX Desktop），或换用其他 agent（如 claude）",
		}
	}

	// --- settings.json ---
	settings := map[string]interface{}{
		"security": map[string]interface{}{
			"auth": map[string]interface{}{
				"selectedType": "gemini-api-key",
			},
		},
	}
	settingsJSON, _ := json.MarshalIndent(settings, "", "  ")
	settingsPath := filepath.Join(geminiDir, "settings.json")
	if err := os.WriteFile(settingsPath, settingsJSON, 0644); err != nil {
		return err
	}

	// --- .env ---
	envContent := "# Gemini CLI provider configuration (written by agent-nexus)\n"
	envContent += "GOOGLE_GEMINI_BASE_URL=" + strings.TrimSuffix(p.BaseURL, "/v1") + "\n"
	envContent += "GEMINI_API_KEY=" + p.APIKey + "\n"
	envContent += "GEMINI_MODEL=" + model + "\n"
	envPath := filepath.Join(geminiDir, ".env")
	if err := os.WriteFile(envPath, []byte(envContent), 0644); err != nil {
		return err
	}

	return nil
}

func (w *geminiWriter) Status(path string) (bool, string) {
	home, _ := os.UserHomeDir()
	if home == "" {
		return false, "未配置代理"
	}
	geminiDir := filepath.Join(home, ".gemini")
	checkPaths := []string{
		filepath.Join(geminiDir, ".env"),
		filepath.Join(geminiDir, "settings.json"),
	}
	proxyMarkers := []string{
		"127.0.0.1", "3688", "sensenova", "platform.sensenova",
		"api.deepseek", "api.siliconflow", "localhost:11434",
	}
	for _, cp := range checkPaths {
		data, err := os.ReadFile(cp)
		if err != nil {
			continue
		}
		s := string(data)
		for _, m := range proxyMarkers {
			if strings.Contains(s, m) {
				return true, "via AI proxy"
			}
		}
	}
	return false, "未配置代理"
}

func (w *geminiWriter) StatusModel(path string) (model, source, notes string) {
	_, source, notes = defaultModelInfo(w.Name())
	home, _ := os.UserHomeDir()
	if home == "" {
		return "", "error", "配置文件未找到"
	}
	envPath := filepath.Join(home, ".gemini", ".env")
	data, err := os.ReadFile(envPath)
	if err != nil {
		return "", source, notes
	}
	s := string(data)
	idx := strings.Index(s, "GEMINI_MODEL=")
	if idx >= 0 {
		rest := s[idx+len("GEMINI_MODEL="):]
		if i := strings.Index(rest, "\n"); i >= 0 {
			return strings.TrimSpace(rest[:i]), source, notes
		}
	}
	return "", source, notes
}
