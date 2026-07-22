package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"agent-nexus/internal/proxy"
)

type piWriter struct{}

func newPiWriter() *piWriter { return &piWriter{} }

func (w *piWriter) Name() string     { return "pi" }
func (w *piWriter) Category() string { return "cli" }
func (w *piWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

// Pi config structure (mirrors ~/.pi/agent/models.json)
type piModelsConfig struct {
	Providers map[string]piProvider `json:"providers"`
}

type piProvider struct {
	BaseURL string        `json:"baseUrl"`
	API     string        `json:"api"`
	APIKey  string        `json:"apiKey"`
	Models  []piModelInfo `json:"models"`
	Compat  *piCompat     `json:"compat,omitempty"`
}

type piModelInfo struct {
	ID        string   `json:"id"`
	Reasoning bool     `json:"reasoning,omitempty"`
	Input     []string `json:"input,omitempty"`
}

type piCompat struct {
	SupportsEagerToolInputStreaming bool `json:"supportsEagerToolInputStreaming"`
	SupportsLongCacheRetention      bool `json:"supportsLongCacheRetention"`
	ForceAdaptiveThinking           bool `json:"forceAdaptiveThinking"`
	AllowEmptySignature             bool `json:"allowEmptySignature"`
}

// Pi settings structure (mirrors ~/.pi/agent/settings.json)
type piSettingsConfig struct {
	DefaultProvider      string `json:"defaultProvider"`
	DefaultModel         string `json:"defaultModel"`
	LastChangelogVersion string `json:"lastChangelogVersion,omitempty"`
}

func (w *piWriter) Configure(path string, p *proxy.Proxy, model string) error {
	if model == "" {
		model = "sensenova-6.7-flash-lite"
	}

	// path is the configPath from discover (e.g. ~/.pi/agent/settings.json)
	// derive models.json from the same directory
	dir := filepath.Dir(path)
	modelsPath := filepath.Join(dir, "models.json")
	settingsPath := filepath.Join(dir, "settings.json")

	// --- Write models.json ---
	// Read existing config if present
	var modelsCfg piModelsConfig
	if data, err := os.ReadFile(modelsPath); err == nil {
		if err := json.Unmarshal(data, &modelsCfg); err != nil {
			return err
		}
	} else {
		modelsCfg.Providers = make(map[string]piProvider)
	}
	// Ensure Providers map is initialized (may be nil after unmarshal)
	if modelsCfg.Providers == nil {
		modelsCfg.Providers = make(map[string]piProvider)
	}

	// Add/update the "ccx" provider
	modelsCfg.Providers["ccx"] = piProvider{
		BaseURL: p.BaseURL,
		API:     "openai-completions",
		APIKey:  p.APIKey,
		Models:  []piModelInfo{{ID: model, Reasoning: true, Input: []string{"text"}}},
		Compat: &piCompat{
			SupportsEagerToolInputStreaming: false,
			SupportsLongCacheRetention:      true,
			ForceAdaptiveThinking:           true,
			AllowEmptySignature:             true,
		},
	}

	modelsJSON, err := json.MarshalIndent(modelsCfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(modelsPath, modelsJSON, 0644); err != nil {
		return err
	}

	// --- Write settings.json ---
	// Read existing settings if present
	var settingsCfg piSettingsConfig
	if data, err := os.ReadFile(settingsPath); err == nil {
		if err := json.Unmarshal(data, &settingsCfg); err != nil {
			return err
		}
	}

	settingsCfg.DefaultProvider = "ccx"
	settingsCfg.DefaultModel = model

	settingsJSON, err := json.MarshalIndent(settingsCfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(settingsPath, settingsJSON, 0644); err != nil {
		return err
	}

	return nil
}

func (w *piWriter) Status(path string) (bool, string) {
	// Check models.json for the ccx provider config
	dir := filepath.Dir(path)
	modelsPath := filepath.Join(dir, "models.json")
	data, _ := os.ReadFile(modelsPath)
	s := string(data)
	if strings.Contains(s, "127.0.0.1") ||
		strings.Contains(s, "platform.sensenova") || strings.Contains(s, "api.deepseek") ||
		strings.Contains(s, "api.siliconflow") || strings.Contains(s, "localhost:11434") {
		return true, "via AI proxy"
	}
	// Also check settings.json
	settingsPath := filepath.Join(dir, "settings.json")
	data, _ = os.ReadFile(settingsPath)
	s = string(data)
	if strings.Contains(s, "127.0.0.1") ||
	strings.Contains(s, "platform.sensenova") || strings.Contains(s, "api.deepseek") {
		return true, "via AI proxy"
	}
	return false, "未配置代理"
}

func (w *piWriter) StatusModel(path string) (model, source, notes string) {
	_, source, notes = defaultModelInfo(w.Name())
	dir := filepath.Dir(path)
	modelsPath := filepath.Join(dir, "models.json")
	data, err := os.ReadFile(modelsPath)
	if err != nil {
		return "", "error", "配置文件未找到"
	}
	// Parse JSON to find the ccx provider's models
	var cfg piModelsConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "", "error", "JSON 解析失败"
	}
	if p, ok := cfg.Providers["ccx"]; ok && len(p.Models) > 0 {
		return p.Models[0].ID, source, notes
	}
	return "", source, notes
}




