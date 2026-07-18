package agent

import (
	"os"
	"strings"

	"agent-nexus/internal/proxy"
)

type kimiWriter struct{}

func newKimiWriter() *kimiWriter { return &kimiWriter{} }

func (w *kimiWriter) Name() string                     { return "kimi" }
func (w *kimiWriter) Category() string                 { return "cli" }
func (w *kimiWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

func (w *kimiWriter) Configure(path string, p *proxy.Proxy) error {
	// Write a clean Kimi config that lets it discover models from the provider
	// without a default_model constraint that requires [models] entries.
	content := "# Kimi CLI Configuration - CCX Proxy\n" +
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
		"api_key = \"" + p.APIKey + "\"\n\n" +
		"[providers.ccx.models]\n" +
		"default = \"sensenova-6.7-flash-lite\"\n\n" +
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

	return os.WriteFile(path, []byte(content), 0644)
}

func (w *kimiWriter) Status(path string) (bool, string) {
	data, _ := os.ReadFile(path)
	s := string(data)
	if strings.Contains(s, "127.0.0.1") && strings.Contains(s, "3688") {
		return true, "via CCX proxy"
	}
	return false, "未配置代理"
}



