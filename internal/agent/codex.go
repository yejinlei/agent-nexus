package agent

import (
	"os"
	"regexp"
	"strings"
	"agent-nexus/internal/proxy"
)

type codexWriter struct{}

func newCodexWriter() *codexWriter { return &codexWriter{} }

func (w *codexWriter) Name() string     { return "codex" }
func (w *codexWriter) Category() string { return "cli" }
func (w *codexWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

func (w *codexWriter) Configure(path string, p *proxy.Proxy, model string) error {
	if model == "" {
		model = "gpt-5.5"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	content := string(data)

	// Replace or add openai_base_url
	if hasPattern(content, `openai_base_url\s*=\s*".*"`) {
		content = applyPattern(content, `openai_base_url\s*=\s*".*"`, "openai_base_url = \""+p.BaseURL+"\"")
	} else {
		content += "\nopenai_base_url = \"" + p.BaseURL + "\"\n"
	}

	// Replace or add model_provider
	if hasPattern(content, `model_provider\s*=\s*".*"`) {
		content = applyPattern(content, `model_provider\s*=\s*".*"`, "model_provider = \"openai\"")
	} else {
		content += "\nmodel_provider = \"openai\"\n"
	}

	// Replace or add model
	if hasPattern(content, `model\s*=\s*".*"`) {
		content = applyPattern(content, `model\s*=\s*".*"`, "model = \""+model+"\"")
	} else {
		content += "\nmodel = \"" + model + "\"\n"
	}

	// Add ccswitch provider block if missing
	if !strings.Contains(content, "[model_providers.ccswitch]") {
		content += "\n[model_providers.ccswitch]\nname = \"Sensenova\"\nbase_url = \"https://token.sensenova.cn/v1\"\nrequires_openai_auth = false\n"
	}

	// Add API key
	if !strings.Contains(content, "api_key") {
		content += "\napi_key = \"" + p.APIKey + "\"\n"
	}

	return os.WriteFile(path, []byte(content), 0644)
}

func (w *codexWriter) Status(path string) (bool, string) {
	data, _ := os.ReadFile(path)
	s := string(data)
	configured := strings.Contains(s, "127.0.0.1") &&
		(strings.Contains(s, "3688") || strings.Contains(s, "sensenova") ||
			strings.Contains(s, "platform.sensenova") || strings.Contains(s, "api.deepseek") ||
			strings.Contains(s, "api.siliconflow") || strings.Contains(s, "localhost:11434"))
	if configured {
		return true, "via AI proxy"
	}
	return false, "未配置代理"
}

func (w *codexWriter) StatusModel(path string) (model, source, notes string) {
	_, source, notes = defaultModelInfo(w.Name())
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "error", "配置文件未找到"
	}
	s := string(data)
	re := regexp.MustCompile(`model\s*=\s*"([^"]+)"`)
	matches := re.FindStringSubmatch(s)
	if len(matches) > 1 {
		return matches[1], source, notes
	}
	return "", source, notes
}

func hasPattern(content, pattern string) bool {
	re := regexp.MustCompile(pattern)
	return re.MatchString(content)
}

func applyPattern(content, pattern, replacement string) string {
	re := regexp.MustCompile(pattern)
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		if re.MatchString(line) {
			lines[i] = replacement
		}
	}
	return strings.Join(lines, "\n")
}
