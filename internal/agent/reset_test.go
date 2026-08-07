package agent

import (
	"agent-nexus/internal/proxy"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResetters_ImplementsInterface(t *testing.T) {
	reg := NewWriterRegistry()
	for _, w := range reg.All() {
		if w.Name() == "gemini" {
			// Gemini is not in DefaultModels (IsConfigurable=false) but
			// still implements Resetter for --reset coverage.
		}
		if agentResettable := HasResetter(w); !agentResettable {
			t.Errorf("writer %s: expected Resetter, got %T", w.Name(), w)
		}
	}
}

// resetRoundTrip runs Configure(path, p, model) then Reset(path) and
// verifies the config file ends up clean.
func resetRoundTrip(t *testing.T, writerName string, preExisting string) {
	reg := NewWriterRegistry()
	w := reg.Get(writerName)
	if w == nil {
		t.Fatalf("writer %s not found", writerName)
	}
	resetter, ok := w.(Resetter)
	if !ok {
		t.Fatalf("writer %s does not implement Resetter", writerName)
	}
	p := &proxy.Proxy{
		BaseURL: "http://127.0.0.1:3688/v1",
		APIKey:  "ccx-key",
		Port:    3688,
		Source:  proxy.ProxyTypeCCX,
	}

	tmp := t.TempDir()
	var cfgPath string

	switch writerName {
	case "claude":
		cfgPath = filepath.Join(tmp, ".claude", "settings.json")
		os.MkdirAll(filepath.Dir(cfgPath), 0755)
		if preExisting != "" {
			os.WriteFile(cfgPath, []byte(preExisting), 0644)
		} else {
			os.WriteFile(cfgPath, []byte("{}"), 0644)
		}
	case "codex":
		cfgPath = filepath.Join(tmp, "Codex", "config.toml")
		os.MkdirAll(filepath.Dir(cfgPath), 0755)
		if preExisting != "" {
			os.WriteFile(cfgPath, []byte(preExisting), 0644)
		} else {
			os.WriteFile(cfgPath, []byte("model = \"old-model\"\n"), 0644)
		}
	case "opencode":
		cfgPath = filepath.Join(tmp, ".config", "opencode", "opencode.jsonc")
		os.MkdirAll(filepath.Dir(cfgPath), 0755)
		os.WriteFile(cfgPath, []byte("{}"), 0644)
	case "openclaw":
		cfgPath = filepath.Join(tmp, ".openclaw", "openclaw.json")
		os.MkdirAll(filepath.Dir(cfgPath), 0755)
		os.WriteFile(cfgPath, []byte(`{"models":{"providers":{}}}`), 0644)
	case "kimi":
		cfgPath = filepath.Join(tmp, ".kimi-code", "config.toml")
		os.MkdirAll(filepath.Dir(cfgPath), 0755)
		os.WriteFile(cfgPath, []byte("[model]\n"), 0644)
	case "openclaude":
		t.Skip("openclaude Reset requires UserHomeDir, skipped in unit test")
	default:
		t.Fatalf("unsupported round-trip for %s", writerName)
	}

	// Configure
	if tw, ok := w.(TieredConfigWriter); ok {
		if err := tw.ConfigureTiered(cfgPath, p, map[string]string{"default": "myccx/glm-5.2"}); err != nil {
			t.Fatalf("ConfigureTiered(%s): %v", writerName, err)
		}
	} else {
		if err := w.Configure(cfgPath, p, "gpt-5.5"); err != nil {
			t.Fatalf("Configure(%s): %v", writerName, err)
		}
	}

	// Status must report configured after Configure
	configured, _ := w.Status(cfgPath)
	if !configured {
		t.Fatalf("Status(%s) should be configured after Configure", writerName)
	}

	// Reset
	auxFiles, resetErr := resetter.Reset(cfgPath)
	if resetErr != nil {
		t.Fatalf("Reset(%s): %v", writerName, resetErr)
	}

	// Delete auxiliary files before checking Status (some agents check aux
	// files for proxy markers, e.g. openclaude checks .openclaude-env).
	for _, p := range auxFiles {
		if _, err := os.Stat(p); err == nil {
			_ = os.Remove(p)
		}
	}

	// Status must report NOT configured after Reset
	afterConfigured, _ := w.Status(cfgPath)
	if afterConfigured {
		t.Fatalf("Status(%s) should NOT be configured after Reset", writerName)
	}

	// (already deleted aux above)
}

func TestResetter_RoundTrip_Claude(t *testing.T) {
	resetRoundTrip(t, "claude", "")
}

func TestResetter_RoundTrip_Claude_WithExisting(t *testing.T) {
	resetRoundTrip(t, "claude", `{"env":{"SOME_KEY":"val"},"model":"sonnet"}`)
}

func TestResetter_RoundTrip_Codex(t *testing.T) {
	resetRoundTrip(t, "codex", "")
}

func TestResetter_RoundTrip_Codex_WithExisting(t *testing.T) {
	existing := `model_provider = "openai"
model = "gpt-5.5"
wire_api = "responses"

[model_providers.ccswitch]
name = "old"
base_url = "old"
wire_api = "old"

[tui.model_availability_nux]
"gpt-5.6-sol" = 2
`
	resetRoundTrip(t, "codex", existing)
}

func TestResetter_RoundTrip_OpenCode(t *testing.T) {
	resetRoundTrip(t, "opencode", "")
}

func TestResetter_RoundTrip_OpenClaw(t *testing.T) {
	resetRoundTrip(t, "openclaw", "")
}

func TestResetter_RoundTrip_Kimi(t *testing.T) {
	resetRoundTrip(t, "kimi", "")
}

// TestCodex_Reset_PreservesUserSections confirms that a Reset on codex's
// config.toml removes our injected keys but leaves user-owned sections
// (like [tui.model_availability_nux]) and user-owned top-level keys intact.
func TestCodex_Reset_PreservesUserSections(t *testing.T) {
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "Codex", "config.toml")
	os.MkdirAll(filepath.Dir(cfgPath), 0755)
	seed := `wire_api = "responses"

[model_providers.ccswitch]
name = "ccswitch"
base_url = "http://localhost/v1"
wire_api = "responses"
requires_openai_auth = true

[tui.model_availability_nux]
"gpt-5.6-sol" = 2

[windows]
sandbox = "elevated"
`
	os.WriteFile(cfgPath, []byte(seed), 0644)

	w := NewWriterRegistry().Get("codex").(*codexWriter)
	if _, err := w.Reset(cfgPath); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	out, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read after Reset: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "model_availability_nux") {
		t.Errorf("user section [tui.model_availability_nux] was removed: %s", s)
	}
	if !strings.Contains(s, "gpt-5.6-sol") {
		t.Errorf("user gpt-5.6-sol entry was removed: %s", s)
	}
	if !strings.Contains(s, "sandbox") {
		t.Errorf("user [windows] section was removed: %s", s)
	}
	if strings.Contains(s, "ccswitch") {
		t.Errorf("[model_providers.ccswitch] was not removed: %s", s)
	}
	// wire_api is a top-level key we own -> should be removed
	if strings.HasPrefix(s, "wire_api") {
		// Actually wire_api at top-level is NOT owned by us (model_providers.ccswitch
		// owns wire_api inside its section). Our topLevelSpec owns
		// openai_base_url, model_provider, model, api_key.
	}
}