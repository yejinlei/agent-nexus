package agent

import (
	"agent-nexus/internal/proxy"
	"agent-nexus/internal/sniff"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type codexWriter struct{}

func newCodexWriter() *codexWriter { return &codexWriter{} }

func (w *codexWriter) Name() string                     { return "codex" }
func (w *codexWriter) Category() string                 { return "cli" }
func (w *codexWriter) CanConfigure(_ *proxy.Proxy) bool { return true }

// Configure writes a Codex-compatible config.
//
// Codex has two config surfaces:
//  - ~/.codex/config.toml  (model/base_url, wire_api, provider blocks)
//  - ~/.codex/auth.json    (OPENAI_API_KEY)
//
// A naive WriteFile clobbers config.toml and destroys codex-owned sections
// like [tui.model_availability_nux] (u32 values), [projects.*] trust
// levels, and [windows] sandbox settings. Codex rejects a clobbered file
// (e.g. "invalid type: string \"sk-...\", expected u32 in
// tui.model_availability_nux").
//
// Fix: merge in-place — preserve every existing section, only touch the
// keys we need (top-level, [model_providers.ccswitch], auth.json).
func (w *codexWriter) Configure(path string, p *proxy.Proxy, model string) error {
	if model == "" {
		model = modelDefault(w.Name())
	}

	// Probe Responses API support on this endpoint.
	probe := sniff.ResponsesProbe(p.BaseURL, p.APIKey)
	if !probe {
		fmt.Println("  ⚠ codex: 上游可能不支持 Responses API (/v1/responses)，配置仍将写入")
		fmt.Println("    若 codex 运行时出错，请换用其他 agent（如 claude）")
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	if err := w.mergeConfigTOML(path, p, model); err != nil {
		return err
	}
	if err := w.writeAuthJSON(dir, p.APIKey); err != nil {
		return err
	}

	return nil
}

// mergeConfigTOML merges our keys into config.toml in-place, preserving
// all existing sections. Codex's config is large and evolves; we only
// claim responsibility for: top-level model/provider keys, a single
// [model_providers.ccswitch] block, and a top-level api_key (compat).
func (w *codexWriter) mergeConfigTOML(path string, p *proxy.Proxy, model string) error {
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	lines := strings.Split(string(data), "\n")
	merged := lines // copy

	// 1) Merge top-level keys into the leading unsectioned block.
	top := topLevelSpec(p, model)
	merged = mergeUnsectionedBlock(merged, top)

	// 2) Merge/replace [model_providers.ccswitch] block.
	// NOTE: requires_openai_auth must be a bare TOML boolean (true), not the
	// quoted string "true" — codex's TOML parser rejects the latter with
	// "invalid type: string \"true\", expected a boolean".
	prov := map[string]string{
		"name":                 "Sensenova CC-Switch",
		"base_url":             p.BaseURL,
		"wire_api":             "responses",
		"requires_openai_auth": "true",
	}
	merged = mergeSection(merged, "model_providers.ccswitch", prov)

	// 3) Ensure top-level api_key exists (after provider block, so it is
	// unambiguously top-level and not nested under the section).
	merged = ensureTopLevelKey(merged, "api_key", p.APIKey)

	// 4) Drop a trailing blank only if file didn't end with one — keep style.
	out := strings.Join(merged, "\n")
	if len(merged) > 0 && merged[len(merged)-1] != "" {
		out += "\n"
	}

	if err := os.WriteFile(path, []byte(out), 0644); err != nil {
		return err
	}
	return nil
}

// topLevelSpec returns the keys we claim at the top level of config.toml.
func topLevelSpec(p *proxy.Proxy, model string) map[string]string {
	return map[string]string{
		"openai_base_url": p.BaseURL,
		"model_provider":  "openai",
		"model":           model,
	}
}

// unsectioned returns the prefix lines that belong to the (implicit)
// top-level TOML table, i.e. up to the first [section] header.
func mergeUnsectionedBlock(merged []string, kv map[string]string) []string {
	secIdx := len(merged)
	for i, line := range merged {
		if isSectionHeader(line) {
			secIdx = i
			break
		}
	}

	// Collect existing unsectioned key-value lines (skip blanks/comments).
	existing := make(map[string]int) // key -> line index
	for i := 0; i < secIdx; i++ {
		k := kvKey(merged[i])
		if k != "" {
			existing[k] = i
		}
	}

	for k, v := range kv {
		idx, ok := existing[k]
		if !ok {
			// Insert at the end of the unsectioned block.
			merged = insertAt(merged, secIdx, fmt.Sprintf("%s = %q", k, v))
			secIdx++
		} else {
			merged[idx] = fmt.Sprintf("%s = %q", k, v)
		}
	}
	return merged
}

// mergeSection replaces or appends a named TOML section with the given
// key-value pairs, preserving the rest of the file.
func mergeSection(lines []string, sectionName string, kv map[string]string) []string {
	start, end := findSection(lines, sectionName)
	if start < 0 {
		// Section doesn't exist — append it at the very end.
		out := make([]string, len(lines))
		copy(out, lines)
		body := make([]string, 0, len(kv)+2)
		body = append(body, fmt.Sprintf("[%s]", sectionName))
		for k, v := range kv {
			body = append(body, fmt.Sprintf("%s = %s", k, tomlValue(v)))
		}
		return append(out, append(body, "")...)
	}

	// Build replacement body for the section.
	body := make([]string, 0, len(kv)+2)
	body = append(body, fmt.Sprintf("[%s]", sectionName))
	for k, v := range kv {
		body = append(body, fmt.Sprintf("%s = %s", k, tomlValue(v)))
	}
	body = append(body, "")

	// Splice.
	head := lines[:start]
	tail := lines[end:]
	return append(head, append(body, tail...)...)
}

// tomlValue emits a bare boolean for true/false, otherwise a quoted string.
// Codex expects `requires_openai_auth = true` (boolean) and rejects `"true"`.
func tomlValue(v string) string {
	if v == "true" || v == "false" {
		return v
	}
	return fmt.Sprintf("%q", v)
}

// ensureTopLevelKey guarantees a top-level api_key line exists, placed
// after any section headers. This is the safest place so it is never
// interpreted as belonging to the previous section.
func ensureTopLevelKey(lines []string, key, val string) []string {
	// If it already exists somewhere, update the last occurrence and return.
	for i := len(lines) - 1; i >= 0; i-- {
		if kvKey(lines[i]) == key {
			lines[i] = fmt.Sprintf("%s = %q", key, val)
			return lines
		}
	}
	// Append at end of file.
	return append(lines, fmt.Sprintf("%s = %q", key, val), "")
}

// findSection locates the start/end line of a TOML section. Returns (-1, -1)
// if absent. The end index is the first line AFTER the section body (either
// the next [header] or end of file, plus any blank separator consumed).
func findSection(lines []string, name string) (int, int) {
	target := "[" + name + "]"
	for i, line := range lines {
		if normLine(line) == target {
			// Find end: next section header.
			end := len(lines)
			for j := i + 1; j < len(lines); j++ {
				if isSectionHeader(lines[j]) {
					end = j
					break
				}
			}
			return i, end
		}
	}
	return -1, -1
}

// normLine strips whitespace and stray '\r' so CRLF files still compare
// cleanly against our '\n'-split lines.
func normLine(line string) string {
	return strings.ReplaceAll(strings.TrimSpace(line), "\r", "")
}

// writeAuthJSON writes ~/.codex/auth.json so codex actually picks up the
// API key (codex reads OPENAI_API_KEY from auth.json, not config.toml).
func (w *codexWriter) writeAuthJSON(dir string, apiKey string) error {
	authPath := filepath.Join(dir, "auth.json")
	auth := map[string]string{
		"OPENAI_API_KEY": apiKey,
		"auth_mode":      "apikey",
	}
	data, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(authPath, data, 0644)
}

func (w *codexWriter) Status(path string) (bool, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, "未配置代理"
	}
	s := string(data)
	if contains(s, "openai_base_url") && contains(s, "model_provider") {
		return true, "via AI proxy"
	}
	return false, "未配置代理"
}

func (w *codexWriter) StatusModel(path string) (model, source, notes string) {
	_, source, notes = defaultModelInfo(w.Name())
	return "", source, notes
}

// ---- minimal TOML helpers (no external dependency) ----

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func isSectionHeader(line string) bool {
	t := strings.TrimSpace(line)
	return len(t) >= 2 && t[0] == '[' && t[len(t)-1] == ']'
}

// kvKey returns the key from a "key = ..." line, or "" if not a kv line.
func kvKey(line string) string {
	t := strings.TrimSpace(line)
	if strings.HasPrefix(t, "#") || strings.HasPrefix(t, "//") {
		return ""
	}
	i := strings.Index(t, "=")
	if i <= 0 {
		return ""
	}
	return strings.TrimSpace(t[:i])
}

func insertAt(lines []string, idx int, v string) []string {
	n := len(lines) + 1
	out := make([]string, n)
	copy(out, lines[:idx])
	out[idx] = v
	copy(out[idx+1:], lines[idx:])
	return out
}
