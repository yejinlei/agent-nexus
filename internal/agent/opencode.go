package agent

import (
	"agent-nexus/internal/proxy"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type openCodeWriter struct{}

func newOpenCodeWriter() *openCodeWriter { return &openCodeWriter{} }

func (w *openCodeWriter) Name() string                     { return "opencode" }
func (w *openCodeWriter) Category() string                 { return "cli" }
func (w *openCodeWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

func (w *openCodeWriter) Configure(path string, p *proxy.Proxy, model string) error {
	if model == "" {
		model = modelDefault(w.Name())
	}

	// Read existing config to preserve any user customisations.
	var cfg map[string]interface{}
	data, err := os.ReadFile(path)
	if err != nil {
		cfg = make(map[string]interface{})
	} else if unmarshalErr := json.Unmarshal(data, &cfg); unmarshalErr != nil {
		cfg = make(map[string]interface{})
	}

	providerID := "myccx"
	modelRef := providerID + "/" + model

	// Build provider block with apiKey in options (required for custom providers).
	providerBlock := map[string]interface{}{
		"npm":  "@ai-sdk/openai-compatible",
		"name": providerID,
		"options": map[string]interface{}{
			"baseURL": p.BaseURL,
			"apiKey":  p.APIKey,
		},
		"models": map[string]interface{}{
			model: map[string]interface{}{"name": model},
		},
	}

	provMap := map[string]interface{}{
		providerID: providerBlock,
	}

	cfg["$schema"] = "https://opencode.ai/config.json"
	cfg["model"] = modelRef
	cfg["small_model"] = providerID + "/deepseek-v4-flash"
	cfg["provider"] = provMap

	// Also store credentials in ~/.local/share/opencode/auth.json
	// (OpenCode requires auth credentials for non-builtin providers).
	home, _ := os.UserHomeDir()
	if home != "" {
		authDir := filepath.Join(home, ".local", "share", "opencode")
		_ = os.MkdirAll(authDir, 0755)
		authFile := filepath.Join(authDir, "auth.json")
		var auth map[string]interface{}
		if authData, authErr := os.ReadFile(authFile); authErr == nil {
			_ = json.Unmarshal(authData, &auth)
		} else {
			auth = make(map[string]interface{})
		}
		// Store as a credential entry keyed by provider name.
		auth[providerID] = map[string]interface{}{
			"apiKey":  p.APIKey,
			"baseURL": p.BaseURL,
		}
		authBytes, _ := json.MarshalIndent(auth, "", "  ")
		_ = os.WriteFile(authFile, authBytes, 0644)
	}

	out, marshalErr := json.MarshalIndent(cfg, "", "  ")
	if marshalErr != nil {
		return marshalErr
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(path, out, 0644)
}

func (w *openCodeWriter) Status(path string) (bool, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, "未配置代理"
	}
	s := string(data)
	if strings.Contains(s, "127.0.0.1") && strings.Contains(s, "3688") {
		return true, "via CCX proxy"
	}
	return false, "未配置代理"
}

func (w *openCodeWriter) StatusModel(path string) (model, source, notes string) {
	_, source, notes = defaultModelInfo(w.Name())
	modelName, found := extractModelFromConfig(path)
	if found {
		return modelName, source, notes
	}
	return "", source, notes
}

// modelDefault returns the canonical default model for this writer's agent
// from the central shared.DefaultModels map.
