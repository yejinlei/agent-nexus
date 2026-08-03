package agent

import (
	"agent-nexus/internal/proxy"
	"agent-nexus/internal/sniff"
	"os"
	"path/filepath"
)

type codexWriter struct{}

func newCodexWriter() *codexWriter { return &codexWriter{} }

func (w *codexWriter) Name() string                     { return "codex" }
func (w *codexWriter) Category() string                 { return "cli" }
func (w *codexWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

// Codex uses wire_api = "responses" which requires the upstream
// endpoint to support OpenAI Responses API (/v1/responses).
// Many endpoints (SenseNova, Anthropic-gateway, etc.) only support
// /v1/chat/completions or /v1/messages.
//
// Configure probes the endpoint's /v1/responses path. If it responds
// successfully, config is written with wire_api = "responses". Otherwise
// configure is refused with a clear warning.
func (w *codexWriter) Configure(path string, p *proxy.Proxy, model string) error {
	if model == "" {
		model = modelDefault(w.Name())
	}

	// Probe Responses API support on this endpoint.
	probe := sniff.ResponsesProbe(p.BaseURL, p.APIKey)
	if !probe {
		return &ErrProtocolIncompatible{
			Agent:    "codex",
			BaseURL:  p.BaseURL,
			Reason:   "需要 Responses API (/v1/responses)",
			Fallback: "使用支持 Responses API 的代理（如 CCX Desktop），或换用其他 agent（如 claude）",
		}
	}

	content :=
		"openai_base_url = \"" + p.BaseURL + "\"\n" +
			"model_provider = \"ccswitch\"\n" +
			"model = \"" + model + "\"\n" +
			"\n" +
			"[model_providers.ccswitch]\n" +
			"name = \"Sensenova CC-Switch\"\n" +
			"base_url = \"" + p.BaseURL + "\"\n" +
			"api_key = \"" + p.APIKey + "\"\n" +
			"wire_api = \"responses\"\n" +
			"requires_openai_auth = false\n"

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}

	return nil
}

func (w *codexWriter) Status(path string) (bool, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, "未配置代理"
	}
	s := string(data)
	// Codex is configured when it has a non-default openai_base_url
	// (pointing at a local proxy or an external gateway).
	if contains(s, "openai_base_url") && contains(s, "model_provider") {
		return true, "via AI proxy"
	}
	return false, "未配置代理"
}

func (w *codexWriter) StatusModel(path string) (model, source, notes string) {
	_, source, notes = defaultModelInfo(w.Name())
	return "", source, notes
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// modelDefault returns the canonical default model for this writer's agent
// from the central shared.DefaultModels map.
