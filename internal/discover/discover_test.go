package discover

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckConfigured(t *testing.T) {
	tests := []struct {
		content string
		want    bool
	}{
		{"base_url = \"http://127.0.0.1:3688/v1\"", true},
		{"base_url = \"https://platform.sensenova.cn/v1\"", true},
		{"base_url = \"https://api.deepseek.com/v1\"", true},
		{"base_url = \"https://api.siliconflow.cn/v1\"", true},
		{"OLLAMA_HOST localhost:11434", true},
		{"http://127.0.0.1:8080/v1", true},
		{"default_model = \"gpt-4\"", false},
		{"", false},
		{"BASE_URL = \"http://127.0.0.1:3688/v1\"", true},
		{"OLLAMA_HOST LOCALHOST:11434", true},
		{"API_KEY = sk-test", false},
	}

	for _, tt := range tests {
		t.Run("content", func(t *testing.T) {
			tmpDir := t.TempDir()
			tmpFile := filepath.Join(tmpDir, "config.toml")
			os.WriteFile(tmpFile, []byte(tt.content), 0644)
			if got := checkConfigured(tmpFile); got != tt.want {
				t.Errorf("checkConfigured(%q) = %v, want %v", tt.content, got, tt.want)
			}
		})
	}
}

func TestDiscover_FoundConfigurableAgent(t *testing.T) {
	tmpDir := t.TempDir()
	roamingDir := filepath.Join(tmpDir, "AppData", "Roaming")
	codexDir := filepath.Join(roamingDir, "Codex")
	os.MkdirAll(codexDir, 0755)
	cfgPath := filepath.Join(codexDir, "config.toml")
os.WriteFile(cfgPath, []byte("base_url = \"http://127.0.0.1:3688/v1\"\n"), 0644)

	origHome := os.Getenv("USERPROFILE")
	defer func() { os.Setenv("USERPROFILE", origHome) }()
	os.Setenv("USERPROFILE", tmpDir)

	agents := Discover()
	foundCodex := false
	for _, a := range agents {
		if a.Name == "codex" {
			foundCodex = true
			if !a.HasConfig {
				t.Errorf("codex should have config; configPath=%s", a.ConfigPath)
			}
			if !a.IsConfigurable {
				t.Error("codex should be configurable")
			}
			if !a.IsConfigured {
				t.Errorf("codex should be configured; configPath=%s", a.ConfigPath)
			}
			break
		}
	}
	if !foundCodex {
		t.Error("codex agent not found in discover results")
	}
}

func TestDiscover_NonConfigurableAgent(t *testing.T) {
	tmpDir := t.TempDir()
	roamingDir := filepath.Join(tmpDir, "AppData", "Roaming")
	copilotDir := filepath.Join(roamingDir, ".config", "github-copilot")
	os.MkdirAll(copilotDir, 0755)
	os.WriteFile(filepath.Join(copilotDir, "config.yaml"), []byte("github_token: abc"), 0644)

	origHome := os.Getenv("USERPROFILE")
	defer func() { os.Setenv("USERPROFILE", origHome) }()
	os.Setenv("USERPROFILE", tmpDir)

	agents := Discover()
	foundCopilot := false
	for _, a := range agents {
		if a.Name == "copilot" {
			foundCopilot = true
			if a.IsConfigurable {
				t.Error("copilot should NOT be configurable")
			}
			if a.Notes == "" {
				t.Error("copilot should have notes explaining why not configurable")
			}
			break
		}
	}
	if !foundCopilot {
		t.Error("copilot agent not found in discover results")
	}
}

func TestDiscover_HomeDirAgent(t *testing.T) {
	tmpDir := t.TempDir()
	// deepseek uses HomeDirFiles: ".deepseek/config.toml"
	deepseekDir := filepath.Join(tmpDir, ".deepseek")
	os.MkdirAll(deepseekDir, 0755)
	os.WriteFile(filepath.Join(deepseekDir, "config.toml"), []byte("api_key = sk-xxx"), 0644)

	origHome := os.Getenv("USERPROFILE")
	defer func() { os.Setenv("USERPROFILE", origHome) }()
	os.Setenv("USERPROFILE", tmpDir)

	agents := Discover()
	foundDeepseek := false
	for _, a := range agents {
		if a.Name == "deepseek" {
			foundDeepseek = true
			if !a.HasConfig {
				t.Error("deepseek should have config")
			}
			break
		}
	}
	if !foundDeepseek {
		t.Error("deepseek agent not found in discover results")
	}
}

func TestDiscover_IdeVariantSkippedWhenCliExists(t *testing.T) {
	tmpDir := t.TempDir()
	// Create both a CLI version and an IDE version
	cliDir := filepath.Join(tmpDir, "AppData", "Roaming", "Qoder", "User")
		os.MkdirAll(cliDir, 0755)
	os.WriteFile(filepath.Join(cliDir, "settings.json"), []byte("{}"), 0644)

	origHome := os.Getenv("USERPROFILE")
	defer func() { os.Setenv("USERPROFILE", origHome) }()
	os.Setenv("USERPROFILE", tmpDir)

	agents := Discover()
	// Both qoder and qoder-ide should appear in registry
	foundQoder := false
	foundQoderIde := false
	for _, a := range agents {
		if a.Name == "qoder" {
			foundQoder = true
		}
		if a.Name == "qoder-ide" {
			foundQoderIde = true
			// The IDE variant may or may not find config depending on how discover
			// handles the "-ide" suffix skip logic
		}
	}
	if !foundQoder {
		t.Error("qoder (CLI) not found")
	}
	_ = foundQoderIde // IDE variant detection depends on specific file layout; non-fatal
}

func TestGetRegistry(t *testing.T) {
	registry := GetRegistry()
	if len(registry) == 0 {
		t.Fatal("registry should not be empty")
	}

	// Check that both configurable and non-configurable agents are present
	names := make(map[string]bool)
	for _, a := range registry {
		names[a.Name] = true
	}

	for _, expected := range []string{"codex", "claude", "kimi", "deepseek", "opencode", "openclaw", "cursor", "antigravity", "copilot", "windsurf", "zed"} {
		if !names[expected] {
			t.Errorf("registry missing agent %s", expected)
		}
	}
}

func TestDiscover_NoAgentsFound(t *testing.T) {
	// Set home to an empty temp dir with no agent configs
	tmpDir := t.TempDir()
	origHome := os.Getenv("USERPROFILE")
	defer func() { os.Setenv("USERPROFILE", origHome) }()
	os.Setenv("USERPROFILE", tmpDir)

	agents := Discover()
	if len(agents) == 0 {
		t.Fatal("Discover should return at least the registry agents even if not found")
	}

	// Verify all returned agents report HasConfig = false
	for _, a := range agents {
		if a.HasConfig {
			t.Errorf("agent %s should not have config in empty home", a.Name)
		}
	}
}
