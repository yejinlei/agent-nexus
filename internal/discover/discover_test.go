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
	// gemini is non-configurable and is in the 11-runtime scope.
	geminiDir := filepath.Join(tmpDir, ".gemini")
	os.MkdirAll(geminiDir, 0755)
	os.WriteFile(filepath.Join(geminiDir, "config.json"), []byte("{}"), 0644)

	origHome := os.Getenv("USERPROFILE")
	defer func() { os.Setenv("USERPROFILE", origHome) }()
	os.Setenv("USERPROFILE", tmpDir)

	agents := Discover()
	foundGemini := false
	for _, a := range agents {
		if a.Name == "gemini" {
			foundGemini = true
			if a.IsConfigurable {
				t.Error("gemini should NOT be configurable")
			}
			break
		}
	}
	if !foundGemini {
		t.Error("gemini agent not found in discover results")
	}
}

func TestDiscover_HomeDirAgent(t *testing.T) {
	tmpDir := t.TempDir()
	// kimi uses HomeDirFiles: ".kimi-code/config.toml" and is in the 11-runtime scope.
	kimiDir := filepath.Join(tmpDir, ".kimi-code")
	os.MkdirAll(kimiDir, 0755)
	os.WriteFile(filepath.Join(kimiDir, "config.toml"), []byte("api_key = sk-xxx"), 0644)

	origHome := os.Getenv("USERPROFILE")
	defer func() { os.Setenv("USERPROFILE", origHome) }()
	os.Setenv("USERPROFILE", tmpDir)

	agents := Discover()
	foundKimi := false
	for _, a := range agents {
		if a.Name == "kimi" {
			foundKimi = true
			if !a.HasConfig {
				t.Error("kimi should have config")
			}
			break
		}
	}
	if !foundKimi {
		t.Error("kimi agent not found in discover results")
	}
}

func TestDiscover_ScopedToInstallableRuntimes(t *testing.T) {
	tmpDir := t.TempDir()
	origHome := os.Getenv("USERPROFILE")
	defer func() { os.Setenv("USERPROFILE", origHome) }()
	os.Setenv("USERPROFILE", tmpDir)

	agents := Discover()
	names := make(map[string]bool)
	for _, a := range agents {
		names[a.Name] = true
	}

	want := []string{"codex", "claude", "kimi", "opencode", "openclaw", "openclaude", "cursor", "hermes", "kiro", "grok", "gemini"}
	for _, n := range want {
		if !names[n] {
			t.Errorf("discover missing agent %s", n)
		}
	}
	for _, n := range []string{"deepseek", "codebuddy", "qoder", "trae", "antigravity", "copilot", "pi", "deveco", "windsurf", "zed", "lmstudio", "clawx"} {
		if names[n] {
			t.Errorf("discover must not contain %s (outside agent list scope)", n)
		}
	}
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
