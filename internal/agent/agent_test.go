package agent

import (
	"agent-nexus/internal/proxy"
	"encoding/json"
	"os"
	"path/filepath"
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
	os.WriteFile(cfgPath, []byte("model = \"old-model\""), 0644)

	w := NewWriterRegistry().Get("codex")
	// Use an unreachable address so ResponsesProbe returns "unknown" (pass)
	// rather than hitting a live server. This test verifies config file
	// writing, not protocol compatibility.
	p := &proxy.Proxy{BaseURL: "http://127.0.0.2:19876/v1", APIKey: "ccx-key", Port: 19876, Source: proxy.ProxyTypeCCX}

	if err := w.Configure(cfgPath, p, ""); err != nil {
		t.Fatalf("Configure error = %v", err)
	}
	data, _ := os.ReadFile(cfgPath)
	s := string(data)
	if !containsAll(s, "openai_base_url", p.BaseURL, "model_provider", "openai") {
		t.Errorf("codex config missing expected fields. Got:\n%s", s)
	}
	if !containsAll(s, "[model_providers.ccswitch]", "base_url") {
		t.Errorf("codex config missing ccswitch provider block. Got:\n%s", s)
	}
	if !containsAll(s, "api_key", "ccx-key") {
		t.Errorf("codex config missing api_key. Got:\n%s", s)
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
	if !containsAll(string(data), "myccx/glm-5.2", "myccx/deepseek-v4-flash") {
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
