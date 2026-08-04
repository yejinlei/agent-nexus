package agent

import (
	"agent-nexus/internal/proxy"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type openclaudeWriter struct{}

func newOpenClaudeWriter() *openclaudeWriter { return &openclaudeWriter{} }

func (w *openclaudeWriter) Name() string                     { return "openclaude" }
func (w *openclaudeWriter) Category() string                 { return "cli" }
func (w *openclaudeWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

func (w *openclaudeWriter) Configure(path string, p *proxy.Proxy, model string) error {
	if model == "" {
		model = modelDefault(w.Name())
	}

	// OpenClaude (https://github.com/gitlawb/openclaude) is a Claude Code fork.
	// It reads provider configuration from ~/.openclaude/providers/*.json and
	// merges them into its runtime provider catalog. It also honours the same
	// env variables as Claude Code for backwards compat.
	//
	// We write:
	//  1) A provider config JSON into ~/.openclaude/providers/sensenova.json
	//     (the format OpenClaude's onboard/provider discovery expects)
	//  2) The legacy .openclaude-env for env-based tooling (independent of path)
	//  3) Merge an "env" block into ~/.openclaude.json so CLI launches that
	//     inherit the parent shell can still pick up the proxy.
	//
	// Note: the `path` argument from discovery could be any of the known
	// config files; we NEVER overwrite the main JSON config with env content.

	home, _ := os.UserHomeDir()
	if home == "" {
		return os.WriteFile(path, []byte("home dir unavailable"), 0644)
	}

	// --- (1) Provider config file (always written to providers/ dir) ---
	providerDir := filepath.Join(home, ".openclaude", "providers")
	if err := os.MkdirAll(providerDir, 0755); err != nil {
		return err
	}
	providerPath := filepath.Join(providerDir, "sensenova.json")
	providerCfg := map[string]interface{}{
		"provider": "sensenova",
		"name":     "SenseNova (CCX Proxy)",
		"baseUrl":  strings.TrimSuffix(p.BaseURL, "/v1"),
		"apiKey":   p.APIKey,
		"api":      "openai-completions",
		"models": []map[string]interface{}{
			{
				"id":            model,
				"name":          "SenseNova " + model,
				"reasoning":     false,
				"input":         []string{"text"},
				"contextWindow": 200000,
				"maxTokens":     4096,
			},
		},
	}
	providerBytes, _ := json.MarshalIndent(providerCfg, "", "  ")
	if err := os.WriteFile(providerPath, providerBytes, 0644); err != nil {
		return err
	}

	// --- (2) Legacy env file (always at ~/.openclaude-env, independent of path) ---
	envFile := filepath.Join(home, ".openclaude-env")
	envContent := "# OpenClaude provider configuration (written by agent-nexus)\n"
	envContent += "CLAUDE_CODE_USE_OPENAI=1\n"
	envContent += "OPENAI_API_KEY=" + p.APIKey + "\n"
	envContent += "OPENAI_BASE_URL=" + p.BaseURL + "\n"
	envContent += "OPENAI_MODEL=" + model + "\n"
	envContent += "ANTHROPIC_API_KEY=" + p.APIKey + "\n"
	envContent += "ANTHROPIC_BASE_URL=" + p.BaseURL + "\n"
	if err := os.MkdirAll(filepath.Dir(envFile), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(envFile, []byte(envContent), 0644); err != nil {
		return err
	}

	// --- (3) Merge env block into ~/.openclaude.json ---
	// OpenClaude stores its config at ~/.openclaude.json (top-level, NOT inside ~/.openclaude/).
	ocJSON := filepath.Join(home, ".openclaude.json")
	// Also try the alternate location some versions use
	if !ocJSONExists(ocJSON) {
		ocJSON = filepath.Join(home, ".openclaude", ".openclaude.json")
	}
	if data, err := os.ReadFile(ocJSON); err == nil {
		var cfg map[string]interface{}
		if unmarshalErr := json.Unmarshal(data, &cfg); unmarshalErr == nil {
			cfg["env"] = map[string]interface{}{
				"ANTHROPIC_API_KEY":      p.APIKey,
				"ANTHROPIC_BASE_URL":     p.BaseURL,
				"OPENAI_API_KEY":         p.APIKey,
				"OPENAI_BASE_URL":        p.BaseURL,
				"OPENAI_MODEL":           model,
				"CLAUDE_CODE_USE_OPENAI": "1",
			}
			cfg["model"] = model
			out, _ := json.MarshalIndent(cfg, "", "  ")
			_ = os.WriteFile(ocJSON, out, 0644)
		}
	}

	return nil
}

func ocJSONExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func (w *openclaudeWriter) Status(path string) (bool, string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return false, "error: " + err.Error()
	}
	checkPaths := []string{
		filepath.Join(home, ".openclaude", "providers", "sensenova.json"),
		filepath.Join(home, ".openclaude", ".openclaude.json"),
		filepath.Join(home, ".openclaude.json"),
		filepath.Join(home, ".openclaude-env"),
		path,
	}
	proxyMarkers := []string{"127.0.0.1", "3688", "sensenova", "platform.sensenova", "api.deepseek", "api.siliconflow", "localhost:11434"}
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

func (w *openclaudeWriter) StatusModel(path string) (model, source, notes string) {
	_, source, notes = defaultModelInfo(w.Name())
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "error", err.Error()
	}
	checkPaths := []string{
		filepath.Join(home, ".openclaude", "providers", "sensenova.json"),
		filepath.Join(home, ".openclaude", ".openclaude.json"),
		filepath.Join(home, ".openclaude.json"),
		filepath.Join(home, ".openclaude-env"),
	}
	for _, cp := range checkPaths {
		data, err := os.ReadFile(cp)
		if err != nil {
			continue
		}
		s := string(data)
		// Try JSON "model" field
		if idx := strings.Index(s, "\"model\""); idx >= 0 {
			colon := strings.Index(s[idx:], ":")
			if colon >= 0 {
				rest := s[idx+colon+1:]
				if quote := strings.Index(rest, "\""); quote >= 0 {
					end := strings.Index(rest[quote+1:], "\"")
					if end >= 0 {
						return strings.TrimSpace(rest[quote+1 : quote+1+end]), source, notes
					}
				}
			}
		}
		// Try env OPENAI_MODEL=
		if idx := strings.Index(s, "OPENAI_MODEL="); idx >= 0 {
			rest := s[idx+len("OPENAI_MODEL="):]
			if i := strings.Index(rest, "\n"); i >= 0 {
				return strings.TrimSpace(rest[:i]), source, notes
			}
		}
	}
	return "", source, notes
}

// modelDefault returns the canonical default model for this writer's agent
// from the central shared.DefaultModels map.
