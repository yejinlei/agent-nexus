package agent

import (
	"agent-nexus/internal/proxy"
	"os"
	"path/filepath"
	"strings"
)

type hermesWriter struct{}

func newHermesWriter() *hermesWriter { return &hermesWriter{} }

func (w *hermesWriter) Name() string                     { return "hermes" }
func (w *hermesWriter) Category() string                 { return "cli" }
func (w *hermesWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

func (w *hermesWriter) Configure(path string, p *proxy.Proxy, model string) error {
	if model == "" {
		model = modelDefault(w.Name())
	}

	// Hermes on Windows stores config at AppData\Local\hermes\config.yaml,
	// on Linux/macOS at ~/.hermes/config.yaml. We search all known paths,
	// read the existing file (preserving all other sections), then rewrite
	// just the model block in the format Hermes expects.
	home, _ := os.UserHomeDir()
	realPaths := []string{path}
	realPaths = append(realPaths,
		filepath.Join(home, "AppData", "Local", "hermes", "config.yaml"),
		filepath.Join(home, ".hermes", "config.yaml"),
	)

	var existingData []byte
	for _, rp := range realPaths {
		if data, err := os.ReadFile(rp); err == nil {
			if existingData == nil {
				existingData = data
			}
		}
	}
	// Always write to the caller-supplied path (the discovery result).
	targetPath := path

	content := string(existingData)
	if content == "" {
		content = "# Hermes Configuration - CCX Proxy\n"
	}

	// Build the model block that Hermes expects.
	newModelBlock := "model:\n" +
		"  default: " + model + "\n" +
		"  provider: custom\n" +
		"  base_url: " + p.BaseURL + "\n" +
		"  api_key: " + p.APIKey + "\n" +
		"  api_mode: chat_completions\n"

	// Replace any existing "model:" block (from column 0 through the next
	// column-0 key) with the new block.
	content = replaceYAMLBlock(content, newModelBlock)

	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(targetPath, []byte(content), 0644)
}

// replaceYAMLBlock removes ALL column-0 "model:" blocks and
// inserts a single newBlock in their place (at the first position found).
func replaceYAMLBlock(content, newBlock string) string {
	lines := strings.Split(content, "\n")

	// Find ALL model block boundaries [start, end) at column 0.
	type blockRange struct{ start, end int }
	var ranges []blockRange
	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimLeft(line, " \t")
		if trimmed != "" && len(line) > 0 && line[0] != ' ' && line[0] != '\t' && strings.HasPrefix(trimmed, "model:") {
			// Found start of a model block; scan forward to next column-0 key.
			start := i
			j := i + 1
			for j < len(lines) {
				next := lines[j]
				nextTrimmed := strings.TrimLeft(next, " \t")
				if nextTrimmed == "" {
					j++
					continue
				}
				if len(next) > 0 && next[0] != ' ' && next[0] != '\t' {
					break // next column-0 key found
				}
				j++
			}
			ranges = append(ranges, blockRange{start, j})
			i = j
		} else {
			i++
		}
	}

	if len(ranges) == 0 {
		// No model blocks; prepend at the top.
		return newBlock + "\n" + content
	}

	// Build a set of indices to skip (all model block lines).
	skip := make(map[int]bool, 0)
	firstStart := ranges[0].start
	for _, r := range ranges {
		for k := r.start; k < r.end; k++ {
			skip[k] = true
		}
	}

	// Build result: insert newBlock at firstStart, skip all model block lines.
	var out []string
	for k := 0; k < len(lines); k++ {
		if k == firstStart {
			// Insert the new model block (firstStart is always a skipped line).
			out = append(out, strings.Split(newBlock, "\n")...)
			continue
		}
		if skip[k] {
			continue
		}
		out = append(out, lines[k])
	}

	return strings.Join(out, "\n")
}

func (w *hermesWriter) Status(path string) (bool, string) {
	// Check the real path if the given path is the old one.
	home, _ := os.UserHomeDir()
	checkPath := path
	if !fileExists(checkPath) {
		alt1 := filepath.Join(home, "AppData", "Local", "hermes", "config.yaml")
		alt2 := filepath.Join(home, ".hermes", "config.yaml")
		if fileExists(alt1) {
			checkPath = alt1
		} else if fileExists(alt2) {
			checkPath = alt2
		} else {
			return false, "未配置代理"
		}
	}
	data, err := os.ReadFile(checkPath)
	if err != nil {
		return false, "未配置代理"
	}
	s := string(data)
	if strings.Contains(s, "sensenova") || strings.Contains(s, "platform.sensenova") ||
		strings.Contains(s, "api.deepseek") || strings.Contains(s, "api.siliconflow") ||
		strings.Contains(s, "127.0.0.1") || strings.Contains(s, "localhost:11434") {
		return true, "via AI proxy"
	}
	return false, "未配置代理"
}

func (w *hermesWriter) StatusModel(path string) (model, source, notes string) {
	_, source, notes = defaultModelInfo(w.Name())
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "error", "配置文件未找到"
	}
	s := string(data)
	if idx := strings.Index(s, "default: "); idx >= 0 {
		line := s[idx+len("default: "):]
		if i := strings.Index(line, "\n"); i >= 0 {
			return strings.TrimSpace(line[:i]), source, notes
		}
		return strings.TrimSpace(line), source, notes
	}
	return "", source, notes
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// modelDefault returns the canonical default model for this writer's agent
// from the central shared.DefaultModels map.
