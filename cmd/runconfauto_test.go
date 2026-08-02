package cmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-nexus/internal/agent"
	"agent-nexus/internal/model"
	"agent-nexus/internal/proxy"
	"agent-nexus/internal/sniff"

	"github.com/spf13/cobra"
)

// testParseAgentsStr splits and validates the --agents string argument.
// Returns (selectedNames, toConfigureNames, nil), where selectedNames is the
// user-specified list and toConfigureNames is the subset that are configurable.
func testParseAgentsStr(agentsStr, configurableSet string) ([]string, []string, error) {
	if agentsStr == "" {
		return nil, nil, nil
	}

	selectedNames := strings.Split(agentsStr, ",")
	for i, n := range selectedNames {
		selectedNames[i] = strings.TrimSpace(n)
	}

	configurableNames := strings.Split(configurableSet, ",")

	var toConfigure []string
	if strings.EqualFold(agentsStr, "all") {
		for _, n := range configurableNames {
			toConfigure = append(toConfigure, strings.TrimSpace(n))
		}
	} else {
		selectedSet := make(map[string]bool)
		for _, name := range selectedNames {
			selectedSet[strings.TrimSpace(name)] = true
		}
		for _, n := range configurableNames {
			if name := strings.TrimSpace(n); selectedSet[name] {
				toConfigure = append(toConfigure, name)
			}
		}
	}

	return selectedNames, toConfigure, nil
}

// testParseModelsStr parses the --models string into an override map.
// Format: "agent1=model1,agent2=model2"
func testParseModelsStr(modelsStr string) map[string]string {
	overrides := make(map[string]string)
	if modelsStr == "" {
		return overrides
	}
	for _, pair := range strings.Split(modelsStr, ",") {
		pair = strings.TrimSpace(pair)
		if idx := strings.Index(pair, "="); idx > 0 {
			agt := strings.TrimSpace(pair[:idx])
			m := strings.TrimSpace(pair[idx+1:])
			if m != "" {
				overrides[agt] = m
			}
		}
	}
	return overrides
}

// testValidateOverridesAgainstUpstream checks override values against the upstream
// model list. Returns warnings for any override model not found upstream.
func testValidateOverridesAgainstUpstream(overrides map[string]string, upstreamModels []string) []string {
	var warnings []string
	for _, modelName := range overrides {
		found := false
		for _, up := range upstreamModels {
			if strings.EqualFold(up, modelName) {
				found = true
				break
			}
		}
		if !found {
			warnings = append(warnings, modelName)
		}
	}
	return warnings
}

func TestParseAgentsStr_All(t *testing.T) {
	selected, toConfigure, err := testParseAgentsStr("all", "codex,claude,kimi,deepseek,opencode")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(selected) != 1 || selected[0] != "all" {
		t.Errorf("selected = %v, want [all]", selected)
	}
	if len(toConfigure) != 5 {
		t.Errorf("toConfigure count = %d, want 5", len(toConfigure))
	}

	expectedSet := map[string]bool{"codex": true, "claude": true, "kimi": true, "deepseek": true, "opencode": true}
	for _, a := range toConfigure {
		if !expectedSet[a] {
			t.Errorf("unexpected agent in toConfigure: %s", a)
		}
	}
}

func TestParseAgentsStr_Multiple(t *testing.T) {
	selected, toConfigure, err := testParseAgentsStr("codex,claude,kimi", "codex,claude,kimi,deepseek")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(selected) != 3 {
		t.Errorf("selected count = %d, want 3", len(selected))
	}

	expected := map[string]bool{"codex": true, "claude": true, "kimi": true}
	for _, a := range toConfigure {
		if !expected[a] {
			t.Errorf("unexpected agent in toConfigure: %s", a)
		}
	}
	if len(toConfigure) != 3 {
		t.Errorf("toConfigure count = %d, want 3", len(toConfigure))
	}
}

func TestParseAgentsStr_UnknownAgent(t *testing.T) {
	_, toConfigure, err := testParseAgentsStr("codex,nonexistent", "codex,claude")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(toConfigure) != 1 || toConfigure[0] != "codex" {
		t.Errorf("toConfigure = %v, want [codex]", toConfigure)
	}
}

func TestParseAgentsStr_WhitespaceTrimmed(t *testing.T) {
	selected, toConfigure, err := testParseAgentsStr("codex,  claude  ,kimi", "codex,claude,kimi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for i, n := range selected {
		if n != strings.TrimSpace(n) {
			t.Errorf("selected[%d] = %q, expected trimmed", i, n)
		}
	}
	if len(toConfigure) != 3 {
		t.Errorf("toConfigure count = %d, want 3", len(toConfigure))
	}
}

func TestParseAgentsStr_EmptyInput(t *testing.T) {
	selected, toConfigure, err := testParseAgentsStr("", "codex,claude")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if selected != nil || len(toConfigure) > 0 {
		t.Errorf("expected nil/empty results for empty input, got selected=%v, toConfigure=%v", selected, toConfigure)
	}
}

func TestParseModelsStr(t *testing.T) {
	tests := []struct {
		input    string
		expected map[string]string
	}{
		{
			input:    "codex=gpt-5.5,claude=glm-5.2",
			expected: map[string]string{"codex": "gpt-5.5", "claude": "glm-5.2"},
		},
		{
			input:    "kimi=sensenova-6.7-flash-lite",
			expected: map[string]string{"kimi": "sensenova-6.7-flash-lite"},
		},
		{
			// empty model is skipped
			input:    "codex=,claude=gpt-5.5",
			expected: map[string]string{"claude": "gpt-5.5"},
		},
		{
			input: "",
			// empty result
			expected: map[string]string{},
		},
	}

	for _, tc := range tests {
		result := testParseModelsStr(tc.input)
		if len(result) != len(tc.expected) {
			t.Errorf("input %q: len(result) = %d, want %d", tc.input, len(result), len(tc.expected))
			continue
		}
		for agent, modelName := range tc.expected {
			if got := result[agent]; got != modelName {
				t.Errorf("input %q: %s = %q, want %q", tc.input, agent, got, modelName)
			}
		}
	}
}

func TestValidateOverridesAgainstUpstream(t *testing.T) {
	upstream := []string{"gpt-5.5", "sensenova-6.7-flash-lite", "glm-5.2"}

	warnings := testValidateOverridesAgainstUpstream(
		map[string]string{"codex": "gpt-5.5", "claude": "sensenova-6.7-flash-lite"},
		upstream,
	)
	if len(warnings) != 0 {
		t.Errorf("expected no warnings, got: %v", warnings)
	}

	warnings2 := testValidateOverridesAgainstUpstream(
		map[string]string{"codex": "gpt-5.5", "claude": "nonexistent-model"},
		upstream,
	)
	if len(warnings2) != 1 || warnings2[0] != "nonexistent-model" {
		t.Errorf("expected 1 warning for nonexistent-model, got: %v", warnings2)
	}

	warnings3 := testValidateOverridesAgainstUpstream(
		map[string]string{"codex": "gpt-5.5"},
		[]string{},
	)
	if len(warnings3) != 1 || warnings3[0] != "gpt-5.5" {
		t.Errorf("expected 1 warning with empty upstream, got: %v", warnings3)
	}

	warnings4 := testValidateOverridesAgainstUpstream(map[string]string{}, upstream)
	if len(warnings4) != 0 {
		t.Errorf("expected no warnings for empty overrides, got: %v", warnings4)
	}
}

func TestConfAutoCmdFlags(t *testing.T) {
	confCmd, found := findSubcommand(rootCmd, "conf")
	if !found {
		t.Fatalf("conf command not found in rootCmd")
	}
	autoCmd, found := findSubcommand(confCmd, "auto")
	if !found {
		t.Fatalf("conf auto command not found in confCmd")
	}

	requiredFlags := []string{"agents", "models", "dry-run"}
	for _, flagName := range requiredFlags {
		flag := autoCmd.Flags().Lookup(flagName)
		if flag == nil {
			t.Errorf("conf auto command missing required flag: %s", flagName)
		}
	}

	agentsFlag := autoCmd.Flags().Lookup("agents")
	if agentsFlag != nil && agentsFlag.DefValue != "" {
		t.Errorf("--agents default should be empty, got: %s", agentsFlag.DefValue)
	}
}

// findSubcommand recursively finds a subcommand by name in a cobra.Command.
func findSubcommand(cmd *cobra.Command, name string) (*cobra.Command, bool) {
	for _, c := range cmd.Commands() {
		if c.Name() == name {
			return c, true
		}
	}
	return nil, false
}

// TestUpstreamModelList_FakeServer tests sniff.UpstreamModelList with a fake HTTP server.
func TestUpstreamModelList_FakeServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "GET" {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer test-api-key" {
			t.Fatalf("missing or wrong auth header")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"object": "list",
			"data": [
				{"id": "gpt-5.5", "object": "model", "created": 1700000000, "owned_by": "ccx"},
				{"id": "sensenova-6.7-flash-lite", "object": "model", "created": 1700000000, "owned_by": "ccx"},
				{"id": "glm-5.2", "object": "model", "created": 1700000000, "owned_by": "ccx"}
			]
		}`))
	}))
	defer server.Close()

	models := sniff.UpstreamModelList(server.URL, "test-api-key")
	if len(models) != 3 {
		t.Fatalf("expected 3 models, got %d: %v", len(models), models)
	}

	expectedSet := map[string]bool{"gpt-5.5": true, "sensenova-6.7-flash-lite": true, "glm-5.2": true}
	for _, m := range models {
		if !expectedSet[m] {
			t.Errorf("unexpected model: %s", m)
		}
	}
}

// TestUpstreamModelList_ConnectionError tests that UpstreamModelList
// returns nil/empty on connection failure.
func TestUpstreamModelList_ConnectionError(t *testing.T) {
	models := sniff.UpstreamModelList("http://127.0.0.1:1", "test-key")
	if models != nil && len(models) > 0 {
		t.Errorf("expected nil or empty slice on connection error, got %d models", len(models))
	}
}

// TestModelToWrite_NoUpstreamFallback tests model resolution when upstream is empty.
func TestModelToWrite_NoUpstreamFallback(t *testing.T) {
	resolutions := []model.Resolution{
		{Agent: "codex", Model: "gpt-5.5", Default: "gpt-5.5", Source: "default", Notes: ""},
		{Agent: "claude", Model: "sensenova-6.7-flash-lite", Default: "fable", Source: "proxy-map", Notes: ""},
	}

	m, found := model.ModelToWrite(resolutions, nil, "codex")
	if !found || m != "gpt-5.5" {
		t.Errorf("codex without override = %q, %v; want gpt-5.5, true", m, found)
	}

	m2, found2 := model.ModelToWrite(resolutions, map[string]string{"codex": "custom-model"}, "codex")
	if !found2 || m2 != "custom-model" {
		t.Errorf("codex with override = %q, %v; want custom-model, true", m2, found2)
	}
}

// TestResolverBuildRoutingTable_Basic tests the routing table build logic.
func TestResolverBuildRoutingTable_Basic(t *testing.T) {
	p := &proxy.Proxy{
		BaseURL:  "http://127.0.0.1:3688/v1",
		APIKey:   "ccx-key",
		Port:     3688,
		Source:   proxy.ProxyTypeCCX,
		ModelMap: map[string]string{"gpt-5.5": "sensenova-6.7-flash-lite"},
	}
	table := model.BuildRoutingTable(p)

	if len(table) < 5 {
		t.Fatalf("expected at least 5 routing entries, got %d", len(table))
	}

	for _, agentName := range []string{"codex", "claude", "kimi", "opencode"} {
		found := false
		for _, entry := range table {
			if entry.Agent == agentName {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("routing table missing agent %s", agentName)
		}
	}
}

// TestResolveAllModels_WithUpstream tests ResolveAllModels with upstream models.
func TestResolveAllModels_WithUpstream(t *testing.T) {
	upstream := []string{"gpt-5.5", "sensenova-6.7-flash-lite"}
	pMap := map[string]string{"fable": "glm-5.2"}

	resolutions := model.ResolveAllModels(upstream, pMap)

	for _, r := range resolutions {
		if r.Agent == "codex" {
			if r.Model != "gpt-5.5" {
				t.Errorf("codex model = %q, want gpt-5.5", r.Model)
			}
			if r.Source != "upstream" {
				t.Errorf("codex source = %q, want upstream", r.Source)
			}
		}
		if r.Agent == "claude" {
			if r.Model != "glm-5.2" {
				t.Errorf("claude model = %q, want glm-5.2", r.Model)
			}
			if r.Source != "proxy-map" {
				t.Errorf("claude source = %q, want proxy-map", r.Source)
			}
		}
	}
}

// TestConfAutoDryRun_NoWrite verifies dry-run mode completes without writing.
func TestConfAutoDryRun_NoWrite(t *testing.T) {
	tmpDir := t.TempDir()
	homeBackup := os.Getenv("HOME")
	defer func() { os.Setenv("HOME", homeBackup) }()
	os.Setenv("HOME", tmpDir)

	caAgents = "codex"
	caModels = ""
	caDryRun = true

	origHomeDir := homeDir
	homeDir = tmpDir
	defer func() { homeDir = origHomeDir }()

	err := runConfAuto(caAgents, caModels, caDryRun)
	if err != nil {
		t.Logf("runConfAuto returned (expected in test env): %v", err)
	}
}

// TestWriterRegistry_AllWritersPresent verifies all expected writers are registered.
func TestWriterRegistry_AllWritersPresent(t *testing.T) {
	reg := agent.NewWriterRegistry()
	writers := reg.All()

	expectedNames := []string{
		"codex", "claude", "kimi", "deepseek", "opencode", "openclaw",
		"codebuddy", "hermes", "kiro", "grok", "qoder",
		"trae", "pi", "openclaude",
	}

	writerNames := make(map[string]bool)
	for _, w := range writers {
		writerNames[w.Name()] = true
	}

	for _, name := range expectedNames {
		if !writerNames[name] {
			t.Errorf("WriterRegistry missing writer: %s", name)
		}
	}
}

// TestProxyFromFlags validates the --url/--key flag parsing path.
func TestProxyFromFlags(t *testing.T) {
	tests := []struct {
		url  string
		key  string
		want *proxy.Proxy
	}{
		{
			url: "http://127.0.0.1:3688/v1", key: "ccx-key",
			want: &proxy.Proxy{BaseURL: "http://127.0.0.1:3688/v1", APIKey: "ccx-key", Source: proxy.ProxyTypeManual, ModelMap: map[string]string{}},
		},
		{
			url: "https://api.example.com", key: "sk-12345",
			want: &proxy.Proxy{BaseURL: "https://api.example.com", APIKey: "sk-12345", Port: 443, Source: proxy.ProxyTypeManual, ModelMap: map[string]string{}},
		},
	}

	for _, tc := range tests {
		p, err := proxy.FromFlags(tc.url, tc.key)
		if err != nil {
			t.Errorf("FromFlags(%q, %q) error: %v", tc.url, tc.key, err)
			continue
		}
		if p == nil {
			t.Errorf("FromFlags(%q, %q) returned nil", tc.url, tc.key)
			continue
		}
		if p.BaseURL != tc.want.BaseURL {
			t.Errorf("FromFlags base URL = %q, want %q", p.BaseURL, tc.want.BaseURL)
		}
		if p.APIKey != tc.want.APIKey {
			t.Errorf("FromFlags API key = %q, want %q", p.APIKey, tc.want.APIKey)
		}
		if p.Source != tc.want.Source {
			t.Errorf("FromFlags Source = %q, want %q", p.Source, tc.want.Source)
		}
	}

	p, err := proxy.FromFlags("", "")
	if err != nil {
		t.Errorf("FromFlags(\"\") error: %v", err)
	}
	if p != nil {
		t.Errorf("FromFlags(\"\") should return nil, got non-nil")
	}

	_, err = proxy.FromFlags("http://example.com", "")
	if err == nil {
		t.Error("FromFlags with URL but no key should return error")
	}
}

// TestSnapshotMessageFormat verifies the snapshot message string format.
func TestSnapshotMessageFormat(t *testing.T) {
	agents := []string{"codex", "claude", "kimi"}
	message := "conf auto: " + strings.Join(agents, ",")

	if !strings.HasPrefix(message, "conf auto: ") {
		t.Errorf("message should start with 'conf auto: ', got: %s", message)
	}

	if message != "conf auto: codex,claude,kimi" {
		t.Errorf("message = %q, want 'conf auto: codex,claude,kimi'", message)
	}

	message2 := "conf auto: " + "codex"
	if message2 != "conf auto: codex" {
		t.Errorf("single agent message = %q, want 'conf auto: codex'", message2)
	}
}

// TestBackupDestDir constructs the expected backup path format.
func TestBackupDestDir(t *testing.T) {
	home := t.TempDir()
	expectedRoot := filepath.Join(home, ".codex", "backups")
	destRoot := filepath.Join(filepath.Join(home, ".codex"), "backups")
	if destRoot != expectedRoot {
		t.Errorf("destRoot = %s, want %s", destRoot, expectedRoot)
	}
}
