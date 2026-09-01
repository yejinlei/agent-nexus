package agent

import (
	"agent-nexus/internal/proxy"
	"encoding/json"
	"fmt"
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
	return w.ConfigureTiered(path, p, map[string]string{"default": model})
}

// ConfigureTiered writes OpenCode's config.json with a primary model and a
// separately-resolved small_model. tiers["default"] (or tiers["opus"]) is used
// as the primary; tiers["haiku"] is used for small_model, falling back to
// default when empty.
func (w *openCodeWriter) ConfigureTiered(path string, p *proxy.Proxy, tiers map[string]string) error {
	model := tiers["default"]
	if model == "" {
		model = tiers["opus"]
	}
	if model == "" {
		model = modelDefault(w.Name())
	}
	if model == "" {
		return fmt.Errorf("未找到 %s 的默认模型", w.Name())
	}

	// Read existing config to preserve any user customisations.
	var cfg map[string]interface{}
	data, err := os.ReadFile(path)
	if err != nil {
		cfg = make(map[string]interface{})
	} else if unmarshalErr := json.Unmarshal(data, &cfg); unmarshalErr != nil {
		cfg = make(map[string]interface{})
	}

	smallModel := tiers["haiku"]
	if smallModel == "" {
		// No explicit resolution: keep the user's existing small_model
		// (minimal-write principle); only seed it from the primary on a
		// first-time write.
		if v, ok := cfg["small_model"].(string); ok && v != "" {
			smallModel = v
		} else {
			smallModel = model
		}
	}

	providerID := "myccx"
	// The model may come in as "myccx/glm-5.2" (with the provider prefix
	// baked into the default) or as a bare model name. Normalise to the bare
	// model name so modelRef doesn't double the prefix.
	bareModel := strings.TrimPrefix(model, providerID+"/")
	if bareModel == "" {
		bareModel = model
	}
	modelRef := providerID + "/" + bareModel
	bareSmall := strings.TrimPrefix(smallModel, providerID+"/")
	if bareSmall == "" {
		bareSmall = smallModel
	}
	smallModelRef := providerID + "/" + bareSmall

	// Build provider block with apiKey in options (required for custom providers).
	providerBlock := map[string]interface{}{
		"npm":  "@ai-sdk/openai-compatible",
		"name": providerID,
		"options": map[string]interface{}{
			"baseURL": p.BaseURL,
			"apiKey":  p.APIKey,
		},
		"models": map[string]interface{}{
			modelRef: map[string]interface{}{"name": modelRef},
		},
	}

	provMap := map[string]interface{}{
		providerID: providerBlock,
	}

	cfg["$schema"] = "https://opencode.ai/config.json"
	cfg["model"] = modelRef
	cfg["small_model"] = smallModelRef
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

// Reset removes the OpenCode config file and its auth file.
func (w *openCodeWriter) Reset(path string) ([]string, error) {
	var toDelete []string
	toDelete = append(toDelete, path)

	home, _ := os.UserHomeDir()
	if home != "" {
		authFile := filepath.Join(home, ".local", "share", "opencode", "auth.json")
		toDelete = append(toDelete, authFile)
	}
	return toDelete, nil
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
