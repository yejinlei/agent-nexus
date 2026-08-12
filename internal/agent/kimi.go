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

// Configure merges our keys into Kimi's existing config.toml in-place,
// preserving all user-written keys and sections (theme, default_thinking,
// hooks, loop_control, background, etc.). agent-nexus claims responsibility
// for only the keys that make the proxy work:
//   - top-level default_model
//   - [providers.ccx]  (type / base_url / api_key)
//   - [models."<model>"]  (provider / model / max_context_size / max_input_size)
//
// Files outside this set survive untouched across re-configures.
func (w *kimiWriter) Configure(path string, p *proxy.Proxy, model string) error {
	if model == "" {
		model = modelDefault(w.Name())
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	// Resolve the two paths we write: the caller-supplied path and a secondary
	// path (~/.kimi-code or ~/.kimi) so whichever Kimi build reads it gets us.
	secondaryPath, err := w.secondaryPath(path, home)
	if err != nil {
		return err
	}
	targets := []string{path, secondaryPath}
	seen := make(map[string]bool)
	targets = uniqueStrings(targets, seen)

	// Seed data: prefer an existing file from the resolved path list so the
	// first write still preserves whatever the user has set up elsewhere.
	var seed []byte
	for _, t := range targets {
		data, err := os.ReadFile(t)
		if err == nil {
			seed = data
			break
		}
	}

	unsectioned := map[string]string{"default_model": model}
	sections := map[string]map[string]string{
		"providers.ccx": {
			"type":             "openai",
			"base_url":         p.BaseURL,
			"api_key":          p.APIKey,
		},
		"models." + quoteTOML(model): {
			"provider":         "ccx",
			"model":            model,
			"max_context_size": "65536",
			"max_input_size":   "65536",
		},
	}

	for _, t := range targets {
		dir := filepath.Dir(t)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		if err := writeMergedTOML(t, seed, unsectioned, sections); err != nil {
			return err
		}
	}
	return nil
}

// writeMergedTOML writes seed (or a fresh file), merges in unsectioned keys
// and sections, and preserves everything else in seed.
func writeMergedTOML(path string, seed []byte,
	unsectioned map[string]string, sections map[string]map[string]string,
) error {
	lines := []string{}
	content := string(seed)
	if content != "" {
		lines = strings.Split(content, "\n")
	}

	if unsectioned != nil && len(unsectioned) > 0 {
		lines = kimiMergeUnsectioned(lines, unsectioned)
	}
	for sec, kvs := range sections {
		lines = kimiMergeSection(lines, sec, kvs)
	}

	out := strings.Join(lines, "\n")
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return os.WriteFile(path, []byte(out), 0644)
}

func kimiMergeUnsectioned(lines []string, kvs map[string]string) []string {
	endUnsectioned := len(lines)
	for i, line := range lines {
		if kimiIsSectionHeader(line) {
			endUnsectioned = i
			break
		}
	}

	unsec := lines[:endUnsectioned]
	seen := make(map[string]bool, len(kvs))

	var updated []string
	for _, line := range unsec {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			updated = append(updated, line)
			continue
		}
		key := kimiKVKey(line)
		if val, ok := kvs[key]; ok {
			updated = append(updated, key+" = \""+val+"\"")
			seen[key] = true
		} else {
			updated = append(updated, line)
		}
	}
	for key, val := range kvs {
		if !seen[key] {
			updated = append(updated, key+" = \""+val+"\"")
		}
	}
	lines = append(updated, lines[endUnsectioned:]...)
	return lines
}

func kimiMergeSection(lines []string, name string, kvs map[string]string) []string {
	start, end := kimiFindSection(lines, name)
	if start >= 0 {
		var updated []string
		updated = append(updated, lines[:start]...)
		seen := make(map[string]bool, len(kvs))
		for i := start; i < end; i++ {
			line := lines[i]
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				updated = append(updated, line)
				continue
			}
			key := kimiKVKey(line)
			if val, ok := kvs[key]; ok {
				updated = append(updated, key+" = \""+val+"\"")
				seen[key] = true
			} else {
				updated = append(updated, line)
			}
		}
		for key, val := range kvs {
			if !seen[key] {
				updated = append(updated, key+" = \""+val+"\"")
			}
		}
		updated = append(updated, lines[end:]...)
		return updated
	}
	// Section doesn't exist; append at the end.
	lines = append(lines, "")
	lines = append(lines, "["+name+"]")
	for key, val := range kvs {
		lines = append(lines, key+" = \""+val+"\"")
	}
	return lines
}

func kimiFindSection(lines []string, name string) (startLine, endLine int) {
	startLine = -1
	endLine = len(lines)
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if kimiIsSectionHeader(line) {
			secName := strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			if secName == name {
				startLine = i
				for j := i + 1; j < len(lines); j++ {
					if kimiIsSectionHeader(lines[j]) {
						endLine = j
						return
					}
				}
				endLine = len(lines)
				return
			}
		}
	}
	return -1, len(lines)
}

func kimiIsSectionHeader(line string) bool {
	trimmed := strings.TrimSpace(line)
	return len(trimmed) >= 2 && trimmed[0] == '[' && trimmed[len(trimmed)-1] == ']'
}

func kimiKVKey(line string) string {
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "#") {
		return ""
	}
	if idx := strings.Index(trimmed, "="); idx > 0 {
		return strings.TrimSpace(trimmed[:idx])
	}
	if idx := strings.Index(trimmed, ":"); idx > 0 {
		return strings.TrimSpace(trimmed[:idx])
	}
	return trimmed
}

func quoteTOML(v string) string {
	return "\"" + v + "\""
}

func (w *kimiWriter) secondaryPath(path, home string) (string, error) {
	if strings.Contains(path, ".kimi-code") {
		return filepath.Join(home, ".kimi", "config.toml"), nil
	}
	if strings.Contains(path, ".kimi/config") {
		return filepath.Join(home, ".kimi-code", "config.toml"), nil
	}
	// Unknown location: write both canonical paths as a best-effort fallback.
	return filepath.Join(home, ".kimi-code", "config.toml"), nil
}

// uniqueStrings deduplicates string slices using the given seen set.
func uniqueStrings(s []string, seen map[string]bool) []string {
	var out []string
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func (w *kimiWriter) Status(path string) (bool, string) {
	home, _ := os.UserHomeDir()
	checkPath := path
	for _, p := range []string{
		filepath.Join(home, ".kimi-code", "config.toml"),
		filepath.Join(home, ".kimi", "config.toml"),
	} {
		if p == path {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			checkPath = p
			break
		}
	}
	data, err := os.ReadFile(checkPath)
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
	// Look for the model entry inside our [models."..."] section.
	marker := "model = \""
	for {
		idx := strings.Index(s, marker)
		if idx < 0 {
			break
		}
		end := strings.Index(s[idx+len(marker):], "\"")
		if end < 0 {
			break
		}
		return s[idx+len(marker) : idx+len(marker)+end], source, notes
	}
	return "", source, notes
}

// keysOwnKimi lists the top-level keys agent-nexus sets in Kimi's config.toml.
var keysOwnKimi = []string{"default_model"}

// sectionsOwnKimi lists the section names agent-nexus sets in Kimi's config.toml.
var sectionsOwnKimi = []string{"providers.ccx"}

// Reset surgically removes agent-nexus injected keys and sections. Any
// user-written keys (theme, default_thinking, hooks, loop_control, etc.)
// are preserved; the file is only deleted if it becomes empty.
func (w *kimiWriter) Reset(path string) ([]string, error) {
	home, _ := os.UserHomeDir()
	targets := w.resetTargets(path, home)

	for _, t := range targets {
		data, err := os.ReadFile(t)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		lines := strings.Split(string(data), "\n")

		// Remove owned top-level keys.
		ownSet := make(map[string]bool, len(keysOwnKimi))
		for _, k := range keysOwnKimi {
			ownSet[k] = true
		}
		secIdx := len(lines)
		for i, line := range lines {
			if kimiIsSectionHeader(line) {
				secIdx = i
				break
			}
		}
		var out []string
		for i, line := range lines {
			if i >= secIdx {
				break
			}
			if ownSet[kimiKVKey(line)] {
				continue
			}
			out = append(out, line)
		}
		out = append(out, lines[secIdx:]...)

		// Remove owned sections.
		for _, sec := range sectionsOwnKimi {
			out = kimiRemoveSection(out, sec)
		}
		// Remove [models."..."] sections whose provider is "ccx" — those are
		// the ones we created. Leave any other [models."..."] sections alone.
		out = kimiRemoveCCXModelSections(out)

		// Collapse trailing blanks.
		for len(out) > 0 && out[len(out)-1] == "" {
			out = out[:len(out)-1]
		}
		if len(out) == 0 {
			_ = os.Remove(t)
		} else {
			content := strings.Join(out, "\n")
			if !strings.HasSuffix(content, "\n") {
				content += "\n"
			}
			_ = os.WriteFile(t, []byte(content), 0644)
		}
	}

	return nil, nil
}

func (w *kimiWriter) resetTargets(path, home string) []string {
	targets := []string{path}
	if home != "" {
		targets = append(targets, filepath.Join(home, ".kimi-code", "config.toml"))
		targets = append(targets, filepath.Join(home, ".kimi", "config.toml"))
	}
	seen := make(map[string]bool)
	return uniqueStrings(targets, seen)
}

func kimiRemoveSection(lines []string, name string) []string {
	start, end := kimiFindSection(lines, name)
	if start < 0 {
		return lines
	}
	head := lines[:start]
	tail := lines[end:]
	if len(tail) > 0 && tail[0] == "" {
		tail = tail[1:]
	}
	return append(head, tail...)
}

// kimiRemoveCCXModelSections removes [models."..."] sections whose
// body contains provider = "ccx" — those are the ones we created.
// Any other [models."..."] sections are preserved.
func kimiRemoveCCXModelSections(lines []string) []string {
	var out []string
	i := 0
	for i < len(lines) {
		line := lines[i]
		if kimiIsSectionHeader(line) && strings.HasPrefix(strings.TrimSpace(line), "[models.") {
			// Find end of this section.
			end := len(lines)
			for j := i + 1; j < len(lines); j++ {
				if kimiIsSectionHeader(lines[j]) {
					end = j
					break
				}
			}
			body := strings.Join(lines[i:end], "\n")
			if strings.Contains(body, "provider = \"ccx\"") ||
				strings.Contains(body, "provider = 'ccx'") {
				i = end
				continue // ours — drop it
			}
			// Not ours — keep.
			for j := i; j < end; j++ {
				out = append(out, lines[j])
			}
			i = end
			continue
		}
		out = append(out, lines[i])
		i++
	}
	return out
}