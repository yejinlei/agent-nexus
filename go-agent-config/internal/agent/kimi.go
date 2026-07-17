package agent

import (
	"os"
	"strings"
	"go-agent-config/internal/proxy"
)

type kimiWriter struct{}

func newKimiWriter() *kimiWriter { return &kimiWriter{} }

func (w *kimiWriter) Name() string     { return "kimi" }
func (w *kimiWriter) Category() string { return "cli" }
func (w *kimiWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

func (w *kimiWriter) Configure(path string, p *proxy.Proxy) error {
	content := "default_model = \"ccx/gpt-5.5\"\ndefault_thinking = true\ndefault_yolo = false\nskip_afk_prompt_injection = false\ndefault_plan_mode = false\ndefault_editor = \"\"\ntheme = \"dark\"\nshow_thinking_stream = true\nhooks = []\nmerge_all_available_skills = true\nextra_skill_dirs = []\ntelemetry = true\n\n[providers.ccx]\ntype = \"openai\"\nbase_url = \"" + p.BaseURL + "\"\napi_key = \"" + p.APIKey + "\"\n\n[providers.ccx.models]\ndefault = \"sensenova-6.7-flash-lite\"\n\n[models]\n\n[loop_control]\nmax_steps_per_turn = 1000\nmax_retries_per_step = 3\nmax_ralph_iterations = 0\nreserved_context_size = 50000\ncompaction_trigger_ratio = 0.85\n\n[background]\nmax_running_tasks = 4\nread_max_bytes = 30000\nnotification_tail_lines = 20\nnotification_tail_chars = 3000\nwait_poll_interval_ms = 500\nworker_heartbeat_interval_ms = 5000\nworker_stale_after_ms = 15000\nkill_grace_period_ms = 2000\nkeep_alive_on_exit = false\nagent_task_timeout_s = 900\nprint_wait_ceiling_s = 3600\n\n[notifications]\nclaim_stale_after_ms = 15000\n\n[services]\n\n[mcp.client]\ntool_call_timeout_ms = 60000\n"
	return os.WriteFile(path, []byte(content), 0644)
}

func (w *kimiWriter) Status(path string) (bool, string) {
	data, _ := os.ReadFile(path)
	s := string(data)
	return strings.Contains(s, "127.0.0.1") && strings.Contains(s, "3688"), "via CCX proxy"
}
