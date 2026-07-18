package model

import (
	"testing"
	"agent-nexus/internal/proxy"
)

func TestBuildRoutingTable_Default(t *testing.T) {
	p := &proxy.Proxy{
		BaseURL:  "http://127.0.0.1:3688/v1",
		APIKey:   "ccx-key",
		Port:     3688,
		Source:   proxy.ProxyTypeCCX,
		ModelMap: map[string]string{},
	}
	table := BuildRoutingTable(p)
	if len(table) < 6 {
		t.Fatalf("expected at least 6 routing entries, got %d", len(table))
	}

	// Check default entries exist
	agents := make(map[string]bool)
	for _, m := range table {
		agents[m.Agent] = true
	}
	for _, expected := range []string{"codex", "claude", "kimi", "deepseek", "opencode", "cursor"} {
		if !agents[expected] {
			t.Errorf("routing table missing agent %s", expected)
		}
	}
}

func TestBuildRoutingTable_WithModelMap(t *testing.T) {
	p := &proxy.Proxy{
		BaseURL: "http://127.0.0.1:3688/v1",
		APIKey:  "ccx-key",
		Port:    3688,
		Source:  proxy.ProxyTypeCCX,
		ModelMap: map[string]string{
			"gpt-5.5": "sensenova-6.7-flash-lite",
			"opus":    "sensenova-u1-fast",
		},
	}
	table := BuildRoutingTable(p)
	proxyEntries := 0
	for _, m := range table {
		if m.Agent == "CCX-proxy" {
			proxyEntries++
		}
	}
	if proxyEntries != 2 {
		t.Errorf("expected 2 CCX-proxy entries, got %d", proxyEntries)
	}
}

func TestBuildRoutingTable_WithDifferentProxySource(t *testing.T) {
	p := &proxy.Proxy{
		BaseURL: "https://api.sensenova.cn/v1",
		APIKey:  "sk-test",
		Port:    443,
		Source:  proxy.ProxyTypeCloud,
		ModelMap: map[string]string{
			"gpt-4": "claude-sonnet-4",
		},
	}
	table := BuildRoutingTable(p)
	proxyEntries := 0
	for _, m := range table {
		if m.Agent == "CCX-proxy" && m.Source == string(proxy.ProxyTypeCloud) {
			proxyEntries++
		}
	}
	if proxyEntries != 1 {
		t.Errorf("expected 1 cloud proxy entry, got %d", proxyEntries)
	}
}

func TestBuildRoutingTable_NilModelMap(t *testing.T) {
	p := &proxy.Proxy{
		BaseURL:  "http://127.0.0.1:3688/v1",
		APIKey:   "ccx-key",
		Port:     3688,
		Source:   proxy.ProxyTypeCCX,
		ModelMap: nil,
	}
	table := BuildRoutingTable(p)
	// Should have only the default 6 entries
	if len(table) != 6 {
		t.Errorf("expected exactly 6 entries for nil ModelMap, got %d", len(table))
	}
	for _, m := range table {
		if m.Agent == "CCX-proxy" {
			t.Errorf("no CCX-proxy entries expected with nil ModelMap, got %v", m)
		}
	}
}

// ---- FindBestModel tests ----

func TestFindBestModel_Found(t *testing.T) {
	table := []ModelMapping{
		{"codex", "gpt-5.5", "sensenova-6.7-flash-lite", "CCX"},
		{"claude", "fable", "glm-5.2", "CCX"},
	}
	model, target := FindBestModel("codex", "", table)
	if model != "gpt-5.5" {
		t.Errorf("model = %q, want gpt-5.5", model)
	}
	if target != "sensenova-6.7-flash-lite" {
		t.Errorf("target = %q, want sensenova-6.7-flash-lite", target)
	}
}

func TestFindBestModel_Fallback(t *testing.T) {
	table := []ModelMapping{
		{"codex", "gpt-5.5", "sensenova-6.7-flash-lite", "CCX"},
	}
	model, target := FindBestModel("unknown-agent", "default-model", table)
	if model != "default-model" {
		t.Errorf("model = %q, want default-model", model)
	}
	if target != "via CCX proxy" {
		t.Errorf("target = %q, want via CCX proxy", target)
	}
}

func TestFindBestModel_NoFallback(t *testing.T) {
	table := []ModelMapping{
		{"codex", "gpt-5.5", "sensenova-6.7-flash-lite", "CCX"},
	}
	model, target := FindBestModel("unknown", "", table)
	if model != "" {
		t.Errorf("model = %q, want empty", model)
	}
	if target != "" {
		t.Errorf("target = %q, want empty", target)
	}
}

func TestFindBestModel_FirstMatchWins(t *testing.T) {
	table := []ModelMapping{
		{"codex", "gpt-5.5", "sensenova-6.7-flash-lite", "CCX"},
		{"codex", "other", "other-target", "manual"},
	}
	model, target := FindBestModel("codex", "", table)
	// First match should win
	if model != "gpt-5.5" {
		t.Errorf("model = %q, want gpt-5.5 (first match)", model)
	}
	if target != "sensenova-6.7-flash-lite" {
		t.Errorf("target = %q, want sensenova-6.7-flash-lite", target)
	}
}

func TestFindBestModel_EmptyTable(t *testing.T) {
	model, target := FindBestModel("codex", "proxy-model", []ModelMapping{})
	if model != "proxy-model" {
		t.Errorf("model = %q, want proxy-model", model)
	}
	if target != "via CCX proxy" {
		t.Errorf("target = %q, want via CCX proxy", target)
	}
}
