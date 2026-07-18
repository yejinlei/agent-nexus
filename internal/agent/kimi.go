package agent

import (
	"os"
	"strings"

	"agent-nexus/internal/model"
	"agent-nexus/internal/proxy"
)

type kimiWriter struct{}

func newKimiWriter() *kimiWriter { return &kimiWriter{} }

func (w *kimiWriter) Name() string                     { return "kimi" }
func (w *kimiWriter) Category() string                 { return "cli" }
func (w *kimiWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

func (w *kimiWriter) Configure(path string, p *proxy.Proxy) error {
	routing := model.BuildRoutingTable(p)
	modelName, targetModel := model.FindBestModel("kimi", "", routing)
	if modelName == "" {
		modelName = "ccx/gpt-5.5"
	}

	content := "# Kimi CLI Configuration - CCX Proxy\n"
	content += "default_model = \"" + modelName + "\"\n"
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
	content += "telemetry = true\n\n"
	content += "[providers.ccx]\n"
	content += "type = \"openai_legacy\"\n"
	content += "base_url = \"" + p.BaseURL + "\"\n"
	content += "api_key = \"" + p.APIKey + "\"\n\n"
	content += "[providers.ccx.models]\n"
	content += "default = \"" + targetModel + "\"\n\n"
	content += "[models]\n"
	content += "\n"
	content += "[\"models.ccx/gpt-5.5\"]\n"
	content += "provider = \"ccx\"\n"
	content += "base_model = \"sensenova-6.7-flash-lite\"\n"
	content += "\n"
	content += "[loop_control]\n"
	content += "max_steps_per_turn = 1000\n"
	content += "max_retries_per_step = 3\n"
	content += "max_ralph_iterations = 0\n"
	content += "reserved_context_size = 50000\n"
	content += "compaction_trigger_ratio = 0.85\n\n"
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
	content += "print_wait_ceiling_s = 3600\n\n"
	content += "[notifications]\n"
	content += "claim_stale_after_ms = 15000\n\n"
	content += "[services]\n\n"
	content += "[mcp.client]\n"
	content += "tool_call_timeout_ms = 60000\n"

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
