package agent

import (
	"agent-nexus/internal/proxy"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- WriterRegistry tests ----

func TestWriterRegistry_Get(t *testing.T) {
	reg := NewWriterRegistry()
	for _, expected := range []string{"codex", "claude", "kimi", "opencode", "openclaw", "hermes", "openclaude"} {
		w := reg.Get(expected)
		if w == nil {
			t.Errorf("registry missing writer for %s", expected)
		} else {
			if w.Name() != expected {
				t.Errorf("writer name = %q, want %s", w.Name(), expected)
			}
		}
	}
}

func TestWriterRegistry_GetNonexistent(t *testing.T) {
	reg := NewWriterRegistry()
	if reg.Get("nonexistent") != nil {
		t.Error("Get for nonexistent name should return nil")
	}
}

func TestWriterRegistry_All(t *testing.T) {
	reg := NewWriterRegistry()
	writers := reg.All()
	if len(writers) == 0 {
		t.Fatal("registry should contain writers")
	}
	// All writers must have unique names
	names := make(map[string]bool)
	for _, w := range writers {
		if names[w.Name()] {
			t.Errorf("duplicate writer name %s", w.Name())
		}
		names[w.Name()] = true
	}
}

func TestWriterRegistry_AllCanConfigure(t *testing.T) {
	reg := NewWriterRegistry()
	p := &proxy.Proxy{
		BaseURL: "http://127.0.0.1:3688/v1",
		APIKey:  "ccx-key",
		Port:    3688,
		Source:  proxy.ProxyTypeCCX,
	}
	for _, w := range reg.All() {
		if !w.CanConfigure(p) {
			t.Errorf("writer %s should be able to configure", w.Name())
		}
	}
}

func TestWriterRegistry_Category(t *testing.T) {
	reg := NewWriterRegistry()

	codexWriter := reg.Get("codex")
	if codexWriter == nil {
		t.Fatal("codex writer not found")
	}
	if codexWriter.Category() != "cli" {
		t.Errorf("codex category = %q, want cli", codexWriter.Category())
	}
}

// ---- Helper: test Configure and Status on each writer ----

func testWriterConfigureAndStatus(t *testing.T, writerName string) {
	reg := NewWriterRegistry()
	w := reg.Get(writerName)
	if w == nil {
		t.Fatalf("writer %s not found in registry", writerName)
	}

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, writerName+".toml")

	// Writers that read existing files need a pre-existing config.
	needsJSON := writerName == "claude" || writerName == "opencode" ||
		writerName == "openclaw"
	needsTOML := writerName == "codex"

	if needsJSON {
		if writerName == "openclaw" {
			// openclaw needs nested models.providers
			cfg := map[string]interface{}{
				"models": map[string]interface{}{
					"providers": map[string]interface{}{},
				},
			}
			data, _ := json.Marshal(cfg)
			os.WriteFile(cfgPath, data, 0644)
		} else {
			os.WriteFile(cfgPath, []byte("{}"), 0644)
		}
	} else if needsTOML {
		// codex reads existing TOML and modifies it
		os.WriteFile(cfgPath, []byte("model = \"old-model\"\n"), 0644)
	}

	// For codex, use an unreachable port so ResponsesProbe passes
	// (connection-refused → unknown → assume OK). This test verifies config
	// file writing, not protocol compatibility.
	baseURL := "http://127.0.0.1:3688/v1"
	port := 3688
	if writerName == "codex" {
		baseURL = "http://127.0.0.2:19876/v1"
		port = 19876
	}

	p := &proxy.Proxy{
		BaseURL: baseURL,
		APIKey:  "ccx-dff3eccc518d9830",
		Port:    port,
		Source:  proxy.ProxyTypeCCX,
		ModelMap: map[string]string{
			"gpt-5.5": "sensenova-6.7-flash-lite",
		},
	}

	err := w.Configure(cfgPath, p, "")
	if err != nil {
		t.Fatalf("Configure(%s) error = %v", writerName, err)
	}

	// Verify file was written
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("config file not written: %v", err)
	}
	if len(data) == 0 {
		t.Errorf("Configure(%s) wrote empty file", writerName)
	}

	// Verify Status reports configured
	configured, detail := w.Status(cfgPath)
	if !configured {
		t.Errorf("Status(%s) should report configured after Configure", writerName)
	}
	if detail == "" {
		t.Errorf("Status(%s) detail should not be empty", writerName)
	}
}

func TestCodexWriter(t *testing.T)    { testWriterConfigureAndStatus(t, "codex") }
func TestClaudeWriter(t *testing.T)   { testWriterConfigureAndStatus(t, "claude") }
func TestKimiWriter(t *testing.T)     { testWriterConfigureAndStatus(t, "kimi") }
func TestOpenCodeWriter(t *testing.T) { testWriterConfigureAndStatus(t, "opencode") }
func TestOpenClawWriter(t *testing.T) { testWriterConfigureAndStatus(t, "openclaw") }
func TestHermesWriter(t *testing.T)   { testWriterConfigureAndStatus(t, "hermes") }

// ---- Individual writer content tests ----

func TestCodexWriter_Content(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.toml")
	os.WriteFile(cfgPath, []byte("model = \"old-model\"\n"), 0644)

	w := NewWriterRegistry().Get("codex")
	p := &proxy.Proxy{BaseURL: "http://127.0.0.2:19876/v1", APIKey: "ccx-key", Port: 19876, Source: proxy.ProxyTypeCCX}

	if err := w.Configure(cfgPath, p, "gpt-5.5"); err != nil {
		t.Fatalf("Configure error = %v", err)
	}
	data, _ := os.ReadFile(cfgPath)
	s := string(data)
	if !containsAll(s, "openai_base_url", p.BaseURL, "model_provider", "openai") {
		t.Errorf("codex config missing expected top-level fields. Got:\n%s", s)
	}
	// merge must preserve the existing [model_providers.ccswitch] key
	// (configure appends/merges; Status still checks openai_base_url).
	if !containsAll(s, "model", "gpt-5.5") {
		t.Errorf("codex config should contain new model. Got:\n%s", s)
	}
	if !strings.Contains(s, "wire_api") {
		t.Errorf("codex config must set wire_api. Got:\n%s", s)
	}

	// auth.json must be written alongside config.toml
	authPath := filepath.Join(tmpDir, "auth.json")
	authData, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("auth.json not written by Configure: %v", err)
	}
	var auth map[string]string
	if err := json.Unmarshal(authData, &auth); err != nil {
		t.Fatalf("auth.json not valid JSON: %v", err)
	}
	if auth["OPENAI_API_KEY"] != "ccx-key" {
		t.Errorf("auth.json OPENAI_API_KEY = %q, want ccx-key", auth["OPENAI_API_KEY"])
	}
}

func TestCodexWriter_MergesExisting(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.toml")
	existing := `openai_base_url = "http://127.0.0.1:3688/v1"
model_provider = "openai"
model = "old-model"

[model_providers.ccswitch]
base_url = "http://localhost:8080/v1"
wire_api = "responses"
requires_openai_auth = true

[tui.model_availability_nux]
"gpt-5.6-sol" = 2

[windows]
sandbox = "elevated"
`
	os.WriteFile(cfgPath, []byte(existing), 0644)

	w := NewWriterRegistry().Get("codex")
	p := &proxy.Proxy{BaseURL: "http://127.0.0.1:3688/v1", APIKey: "new-key", Port: 3688, Source: proxy.ProxyTypeCCX}

	if err := w.Configure(cfgPath, p, "sensenova-6.7-flash-lite"); err != nil {
		t.Fatalf("Configure error = %v", err)
	}
	data, _ := os.ReadFile(cfgPath)
	s := string(data)

	// Our new keys must be present.
	if !containsAll(s, "sensenova-6.7-flash-lite", `wire_api = "responses"`) {
		t.Errorf("new keys missing. Got:\n%s", s)
	}
	// Sections codex owns must survive the merge.
	if !strings.Contains(s, "model_availability_nux") {
		t.Errorf("merge clobbered [tui.model_availability_nux]. Got:\n%s", s)
	}
	if !strings.Contains(s, "gpt-5.6-sol") {
		t.Errorf("merge clobbered gpt-5.6-sol entry. Got:\n%s", s)
	}
	if !strings.Contains(s, `sandbox = "elevated"`) {
		t.Errorf("merge clobbered [windows] sandbox. Got:\n%s", s)
	}
}

func TestClaudeWriter_Content(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "settings.json")
	os.WriteFile(cfgPath, []byte("{}"), 0644)

	w := NewWriterRegistry().Get("claude")
	p := &proxy.Proxy{BaseURL: "http://127.0.0.1:3688/v1", APIKey: "ccx-key", Port: 3688, Source: proxy.ProxyTypeCCX}

	if err := w.Configure(cfgPath, p, ""); err != nil {
		t.Fatalf("Configure error = %v", err)
	}
	data, _ := os.ReadFile(cfgPath)
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}
	env, ok := cfg["env"].(map[string]interface{})
	if !ok {
		t.Fatalf("env should be a map, got %T", cfg["env"])
	}
	if env["ANTHROPIC_BASE_URL"] != "http://127.0.0.1:3688" {
		t.Errorf("ANTHROPIC_BASE_URL = %q, want http://127.0.0.1:3688", env["ANTHROPIC_BASE_URL"])
	}
	if env["ANTHROPIC_AUTH_TOKEN"] != "ccx-key" {
		t.Errorf("ANTHROPIC_AUTH_TOKEN = %q, want ccx-key", env["ANTHROPIC_AUTH_TOKEN"])
	}
	if cfg["model"] != "fable" {
		t.Errorf("model = %v, want fable", cfg["model"])
	}
	// Per-tier model mappings must be set (otherwise tier switch 404s)
	for _, tier := range []string{"OPUS", "SONNET", "HAIKU"} {
		m := "ANTHROPIC_DEFAULT_" + tier + "_MODEL"
		n := "ANTHROPIC_DEFAULT_" + tier + "_MODEL_NAME"
		if env[m] != "fable" {
			t.Errorf("%s = %v, want fable", m, env[m])
		}
		if env[n] != "fable" {
			t.Errorf("%s = %v, want fable", n, env[n])
		}
	}
}

func TestOpenCodeWriter_Content(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "opencode.jsonc")
	os.WriteFile(cfgPath, []byte("{}"), 0644)

	w := NewWriterRegistry().Get("opencode")
	p := &proxy.Proxy{BaseURL: "http://127.0.0.1:3688/v1", APIKey: "ccx-key", Port: 3688, Source: proxy.ProxyTypeCCX}

	if err := w.Configure(cfgPath, p, ""); err != nil {
		t.Fatalf("Configure error = %v", err)
	}
	data, _ := os.ReadFile(cfgPath)
	// The default model (myccx/glm-5.2) is written for both the primary model
	// and small_model; small_model now resolves via the tiered writer instead of
	// a hardcoded fallback.
	if !containsAll(string(data), "myccx/glm-5.2", "small_model") {
		t.Errorf("opencode config missing expected model refs. Got:\n%s", string(data))
	}
}

func TestHermesWriter_Content(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	w := NewWriterRegistry().Get("hermes")
	p := &proxy.Proxy{BaseURL: "http://127.0.0.1:3688/v1", APIKey: "ccx-key", Port: 3688, Source: proxy.ProxyTypeCCX}

	if err := w.Configure(cfgPath, p, ""); err != nil {
		t.Fatalf("Configure error = %v", err)
	}
	data, _ := os.ReadFile(cfgPath)
	s := string(data)
	if !containsAll(s, "model:", "provider: custom", "base_url:", "api_key:", "api_mode: chat_completions", "default:") {
		t.Errorf("hermes config missing expected fields. Got:\n%s", s)
	}
}

func TestWriterConfigure_NonexistentFile(t *testing.T) {
	// All writers should succeed on a nonexistent file by creating one.
	for _, writerName := range []string{
		"codex", "claude", "kimi", "opencode", "openclaw",
		"openclaude", "hermes",
	} {
		w := NewWriterRegistry().Get(writerName)
		if w == nil {
			t.Fatalf("writer %s not found", writerName)
		}
		baseURL := "http://127.0.0.1:3688/v1"
		port := 3688
		if writerName == "codex" {
			baseURL = "http://127.0.0.2:19876/v1"
			port = 19876
		}
		p := &proxy.Proxy{BaseURL: baseURL, APIKey: "ccx-key", Port: port, Source: proxy.ProxyTypeCCX}
		tmp := t.TempDir()
		cfgPath := filepath.Join(tmp, writerName+".toml")
		err := w.Configure(cfgPath, p, "")
		if err != nil {
			t.Errorf("Configure(%s) should succeed creating new file: %v", writerName, err)
		}
	}
}

func TestWriterStatus_NotConfigured(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.toml")
	os.WriteFile(cfgPath, []byte("default_model = \"gpt-4\""), 0644)

	w := NewWriterRegistry().Get("codex")
	if w == nil {
		t.Fatal("codex writer not found")
	}
	configured, detail := w.Status(cfgPath)
	if configured {
		t.Error("Status should report not configured for plain gpt-4 config")
	}
	if detail == "" {
		t.Error("Status detail should not be empty")
	}
}

func TestWriterStatus_FileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "nonexistent.toml")

	w := NewWriterRegistry().Get("codex")
	if w == nil {
		t.Fatal("codex writer not found")
	}
	configured, _ := w.Status(cfgPath)
	if configured {
		t.Error("Status should report not configured for nonexistent file")
	}
}

// ---- Edge case: openclaw with existing nested config ----

func TestOpenClawWriter_ExtendsExisting(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "openclaw.json")
	cfg := map[string]interface{}{
		"models": map[string]interface{}{
			"providers": map[string]interface{}{
				"existing": map[string]interface{}{"id": "existing", "name": "Existing Provider"},
			},
		},
	}
	data, _ := json.Marshal(cfg)
	os.WriteFile(cfgPath, data, 0644)

	w := NewWriterRegistry().Get("openclaw")
	p := &proxy.Proxy{BaseURL: "http://127.0.0.1:3688/v1", APIKey: "ccx-key", Port: 3688, Source: proxy.ProxyTypeCCX}

	if err := w.Configure(cfgPath, p, ""); err != nil {
		t.Fatalf("Configure error = %v", err)
	}

	// Verify both providers exist
	data2, _ := os.ReadFile(cfgPath)
	var result map[string]interface{}
	if err := json.Unmarshal(data2, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}
	providers := result["models"].(map[string]interface{})["providers"].(map[string]interface{})
	if _, ok := providers["sensenova-ccx"]; !ok {
		t.Error("sensenova-ccx provider should be added")
	}
	if _, ok := providers["existing"]; !ok {
		t.Error("existing provider should be preserved")
	}
}

// Helper
func containsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if !containsSubstr(s, sub) {
			return false
		}
	}
	return true
}

func containsSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
