package agent

import (
	"agent-nexus/internal/proxy"
	"os"
	"path/filepath"
	"strings"
)

type kimiWriter struct{}

func newKimiWriter() *kimiWriter { return &kimiWriter{} }

func (w *kimiWriter) Name() string                     { return "kimi" }
func (w *kimiWriter) Category() string                 { return "cli" }
func (w *kimiWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

func kimiConfigContent(p *proxy.Proxy, model string) string {
	// Kimi code CLI: type must be "openai" (not "openai_legacy"),
	// provider name referenced from [models.<model>], default_model is the top-level selector.
	content := "# Kimi CLI Configuration - AI Proxy\n"
	content += "default_model = \"" + model + "\"\n"
	content += "default_thinking = true\n"
	content += "default_yolo = false\n"
	content += "skip_afk_prompt_injection = false\n"
	content += "default_plan_mode = false\n"
	content += "default_editor = \"\"\n"
	content += "theme = \"dark\"\n"
	content += "show_thinking_stream = true\n"
	content += "hooks = []\n"
	content += "merge_all_available_skills = true\n"
	content += "extra_skill_dirs = []\n"
	content += "telemetry = true\n"
	content += "\n"
	content += "[providers.ccx]\n"
	content += "type = \"openai\"\n"
	content += "base_url = \"" + p.BaseURL + "\"\n"
	content += "api_key = \"" + p.APIKey + "\"\n"
	content += "\n"
	content += "[models.\"" + model + "\"]\n"
	content += "provider = \"ccx\"\n"
	content += "model = \"" + model + "\"\n"
	content += "max_context_size = 65536\n"
	content += "max_input_size = 65536\n"
	content += "\n"
	content += "[loop_control]\n"
	content += "max_steps_per_turn = 1000\n"
	content += "max_retries_per_step = 3\n"
	content += "max_ralph_iterations = 0\n"
	content += "reserved_context_size = 50000\n"
	content += "compaction_trigger_ratio = 0.85\n"
	content += "\n"
	content += "[background]\n"
	content += "max_running_tasks = 4\n"
	content += "read_max_bytes = 30000\n"
	content += "notification_tail_lines = 20\n"
	content += "notification_tail_chars = 3000\n"
	content += "wait_poll_interval_ms = 500\n"
	content += "worker_heartbeat_interval_ms = 5000\n"
	content += "worker_stale_after_ms = 15000\n"
	content += "kill_grace_period_ms = 2000\n"
	content += "keep_alive_on_exit = false\n"
	content += "agent_task_timeout_s = 900\n"
	content += "print_wait_ceiling_s = 3600\n"
	content += "\n"
	content += "[notifications]\n"
	content += "claim_stale_after_ms = 15000\n"
	content += "\n"
	content += "[services]\n"
	content += "\n"
	content += "[mcp.client]\n"
	content += "tool_call_timeout_ms = 60000\n"
	return content
}

func (w *kimiWriter) Configure(path string, p *proxy.Proxy, model string) error {
	if model == "" {
		model = modelDefault(w.Name())
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	content := kimiConfigContent(p, model)

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return err
	}

	var secondaryPath string
	switch {
	case strings.Contains(path, ".kimi-code"):
		secondaryPath = filepath.Join(home, ".kimi", "config.toml")
	case strings.Contains(path, ".kimi/config"):
		secondaryPath = filepath.Join(home, ".kimi-code", "config.toml")
	default:
		_ = os.MkdirAll(filepath.Join(home, ".kimi-code"), 0755)
		_ = os.MkdirAll(filepath.Join(home, ".kimi"), 0755)
		return os.WriteFile(filepath.Join(home, ".kimi-code", "config.toml"), []byte(content), 0644)
	}
	_ = os.MkdirAll(filepath.Dir(secondaryPath), 0755)

	return os.WriteFile(secondaryPath, []byte(content), 0644)
}

func (w *kimiWriter) Status(path string) (bool, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, "未配置代理"
	}
	s := string(data)
	configured := strings.Contains(s, "127.0.0.1") ||
		strings.Contains(s, "sensenova") || strings.Contains(s, "platform.sensenova") ||
		strings.Contains(s, "api.deepseek") || strings.Contains(s, "api.siliconflow") ||
		strings.Contains(s, "localhost:11434")
	if configured {
		return true, "via AI proxy"
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
	if idx := strings.Index(s, "model = \""); idx >= 0 {
		end := strings.Index(s[idx+len("model = \""):], "\"")
		if end >= 0 {
			return s[idx+len("model = \"") : idx+len("model = \"")+end], source, notes
		}
	}
	return "", source, notes
}

// modelDefault returns the canonical default model for this writer's agent
// from the central shared.DefaultModels map.
