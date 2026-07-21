package model

import (
    "testing"
    "agent-nexus/internal/proxy"
)

func TestResolveModelForAgent_Upstream(t *testing.T) {
    upstream := []string{"sensenova-6.7-flash-lite", "gpt-5.5", "deepseek-v4-flash"}
    pMap := map[string]string{"fable": "glm-5.2"}

    // sensenova-6.7-flash-lite exists upstream -> upstream
    model, src := ResolveModelForAgent("deepseek", "sensenova-6.7-flash-lite", upstream, pMap)
    if model != "sensenova-6.7-flash-lite" {
        t.Errorf("model = %q, want sensenova-6.7-flash-lite", model)
    }
    if src != "upstream" {
        t.Errorf("source = %q, want upstream", src)
    }
}

func TestResolveModelForAgent_ProxyMap(t *testing.T) {
    upstream := []string{"sensenova-6.7-flash-lite", "gpt-5.5"}
    pMap := map[string]string{"fable": "glm-5.2", "gpt-5.5": "sensenova-6.7-flash-lite"}

    // fable not upstream, but in proxy map -> proxy-map
    model, src := ResolveModelForAgent("claude", "fable", upstream, pMap)
    if model != "glm-5.2" {
        t.Errorf("model = %q, want glm-5.2", model)
    }
    if src != "proxy-map" {
        t.Errorf("source = %q, want proxy-map", src)
    }
}

func TestResolveModelForAgent_Default(t *testing.T) {
    upstream := []string{"gpt-5.5"}
    pMap := map[string]string{}

    // no proxy map, model not upstream -> default
    model, src := ResolveModelForAgent("cursor", "sensenova-6.7-flash-lite", upstream, pMap)
    if model != "sensenova-6.7-flash-lite" {
        t.Errorf("model = %q, want sensenova-6.7-flash-lite", model)
    }
    if src != "default" {
        t.Errorf("source = %q, want default", src)
    }
}

func TestResolveModelForAgent_NilUpstream(t *testing.T) {
    pMap := map[string]string{"fable": "glm-5.2"}

    model, src := ResolveModelForAgent("claude", "fable", nil, pMap)
    if model != "glm-5.2" {
        t.Errorf("model = %q, want glm-5.2", model)
    }
    if src != "proxy-map" {
        t.Errorf("source = %q, want proxy-map", src)
    }
}

func TestResolveAllModels(t *testing.T) {
    upstream := []string{"sensenova-6.7-flash-lite", "gpt-5.5", "myccx/glm-5.2"}
    pMap := map[string]string{"fable": "glm-5.2"}

    resolutions := ResolveAllModels(upstream, pMap)
    if len(resolutions) == 0 {
        t.Fatal("expected non-empty resolutions")
    }

    // codex default = gpt-5.5 -> exists upstream
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

func TestResolveAllModels_BuildRoutingTable(t *testing.T) {
    // BuildRoutingTable with a proxy that has a ModelMap
    p := &proxy.Proxy{
        BaseURL: "http://127.0.0.1:3688/v1",
        APIKey:  "ccx-key",
        Port:    3688,
        Source:  proxy.ProxyTypeCCX,
        ModelMap: map[string]string{
            "gpt-5.5": "sensenova-6.7-flash-lite",
            "fable":   "glm-5.2",
        },
    }
    table := BuildRoutingTable(p)
    if len(table) < 13 {
        t.Fatalf("expected at least 13 routing entries, got %d", len(table))
    }

    proxyCount := 0
    for _, m := range table {
        if m.Agent == "CCX-proxy" {
            proxyCount++
        }
    }
    if proxyCount != 2 {
        t.Errorf("expected 2 CCX-proxy entries, got %d", proxyCount)
    }
}

func TestResolution_NeedRedirect(t *testing.T) {
    r := Resolution{Source: "proxy-map"}
    if !r.NeedRedirect() {
        t.Error("proxy-map should need redirect")
    }
    r2 := Resolution{Source: "upstream"}
    if r2.NeedRedirect() {
        t.Error("upstream should not need redirect")
    }
}

func TestModelToWrite(t *testing.T) {
    resolutions := []Resolution{
        {Agent: "codex", Model: "gpt-5.5", Source: "upstream"},
        {Agent: "claude", Model: "glm-5.2", Source: "proxy-map"},
    }
    overrides := map[string]string{"claude": "custom-model"}

    model, found := ModelToWrite(resolutions, overrides, "codex")
    if !found || model != "gpt-5.5" {
        t.Errorf("codex = %q, %v; want gpt-5.5, true", model, found)
    }

    model, found = ModelToWrite(resolutions, overrides, "claude")
    if !found || model != "custom-model" {
        t.Errorf("claude = %q, %v; want custom-model, true", model, found)
    }

    _, found = ModelToWrite(resolutions, overrides, "unknown")
    if found {
        t.Error("unknown agent should not be found")
    }
}
