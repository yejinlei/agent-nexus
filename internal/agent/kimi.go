package agent

import (
	"os"
	"path/filepath"
	"strings"

	"agent-nexus/internal/proxy"
)

type kimiWriter struct{}

func newKimiWriter() *kimiWriter { return &kimiWriter{} }

func (w *kimiWriter) Name() string                     { return "kimi" }
func (w *kimiWriter) Category() string                 { return "cli" }
func (w *kimiWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

// kimiConfigContent generates the config content shared by both kimi-code and kimi-legacy.
// Includes model at [providers.ccx] level for kimi-legacy compatibility,
// and [providers.ccx.models].default for kimi-code compatibility.
func kimiConfigContent(p *proxy.Proxy, model string) string {
	return "# Kimi CLI Configuration - CCX Proxy\n" +
		"# Default model is auto-selected by Kimi from the ccx provider\n" +
		"default_thinking = true\n" +
		"default_yolo = false\n" +
		"skip_afk_prompt_injection = false\n" +
		"default_plan_mode = false\n" +
		"default_editor = \"\"\n" +
		"theme = \"dark\"\n" +
		"show_thinking_stream = true\n" +
		"hooks = []\n" +
		"merge_all_available_skills = true\n" +
		"extra_skill_dirs = []\n" +
		"telemetry = true\n\n" +
		"[providers.ccx]\n" +
		"type = \"openai_legacy\"\n" +
		"base_url = \"" + p.BaseURL + "\"\n" +
		"api_key = \"" + p.APIKey + "\"\n" +
		"model = \"" + model + "\"\n\n" + // model at provider level for kimi-legacy
		"[providers.ccx.models]\n" +
		"default = \"" + model + "\"\n\n" +
		"[loop_control]\n" +
		"max_steps_per_turn = 1000\n" +
		"max_retries_per_step = 3\n" +
		"max_ralph_iterations = 0\n" +
		"reserved_context_size = 50000\n" +
		"compaction_trigger_ratio = 0.85\n\n" +
		"[background]\n" +
		"max_running_tasks = 4\n" +
		"read_max_bytes = 30000\n" +
		"notification_tail_lines = 20\n" +
		"notification_tail_chars = 3000\n" +
		"wait_poll_interval_ms = 500\n" +
		"worker_heartbeat_interval_ms = 5000\n" +
		"worker_stale_after_ms = 15000\n" +
		"kill_grace_period_ms = 2000\n" +
		"keep_alive_on_exit = false\n" +
		"agent_task_timeout_s = 900\n" +
		"print_wait_ceiling_s = 3600\n\n" +
		"[notifications]\n" +
		"claim_stale_after_ms = 15000\n\n" +
		"[services]\n\n" +
		"[mcp.client]\n" +
		"tool_call_timeout_ms = 60000\n"
}

func (w *kimiWriter) Configure(path string, p *proxy.Proxy, model string) error {
	if model == "" {
		model = "gpt-5.5"
	}
	// Determine user home directory to also write to legacy kimi config path
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	content := kimiConfigContent(p, model)

	// Primary path: write to the discovered config path (kimi-code or kimi)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}

	// Secondary path: also write to the other known kimi config location.
	// This ensures both kimi-code (~/.kimi-code/config.toml) and
	// kimi-legacy (~/.kimi/config.toml) receive the correct config.
	var secondaryPath string
	switch {
	case strings.Contains(path, ".kimi-code"):
		secondaryPath = filepath.Join(home, ".kimi", "config.toml")
	case strings.Contains(path, ".kimi/config"):
		secondaryPath = filepath.Join(home, ".kimi-code", "config.toml")
	default:
		// Unknown path; also try both standard locations
		_ = os.WriteFile(filepath.Join(home, ".kimi-code", "config.toml"), []byte(content), 0644)
		return os.WriteFile(filepath.Join(home, ".kimi", "config.toml"), []byte(content), 0644)
	}

	return os.WriteFile(secondaryPath, []byte(content), 0644)
}

func (w *kimiWriter) Status(path string) (bool, string) {
	data, _ := os.ReadFile(path)
	s := string(data)
	if strings.Contains(s, "127.0.0.1") && strings.Contains(s, "3688") {
		return true, "via CCX proxy"
	}
	return false, "未配置代理"
}

func (w *kimiWriter) StatusModel(path string) (model, source, notes string) {
	_, source, notes = defaultModelInfo(w.Name())
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "error", "配置文件未找到"
	}
	s := string(data)
	// Look for model = "xxx" in the TOML
	if idx := strings.Index(s, "model = \""); idx >= 0 {
		end := strings.Index(s[idx+len("model = \""):], "\"")
		if end >= 0 {
			return s[idx+len("model = \""):idx+len("model = \"")+end], source, notes
		}
	}
	return "", source, notes
}
