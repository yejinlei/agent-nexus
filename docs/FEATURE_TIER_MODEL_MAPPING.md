# Feature: Tier-Based Model Mapping (per-agent, per-tier)

**Branch:** `feat/tier-model-mapping`
**Status:** 📋 Spec — not yet implemented
**Owner (context):** agent-nexus team
**Created:** 2026-08-06

---

## 1. Problem Statement

agent-nexus currently supports **one default model per agent**. This is the
single `model string` that flows through:

```
shared.DefaultModels["<agent>"]
  → model.ResolveModelForAgent()
  → cmd.runconfset.processAgents()
  → ConfigWriter.Configure(path, p, model)
```

Two real-world gaps:

1. **Claude has three tiers (opus / sonnet / haiku)** but all three are
   written to the *same* model value today. Switching tiers sends an unknown
   model name to the upstream proxy → 404.
2. **OpenCode has two model roles** (`model` + `small_model`) but
   `small_model` is hardcoded to a fixed upstream ID and never participates in
   resolution.

The goal of this feature is to make model assignment **per-agent × per-role**,
so each logical role gets a distinct, resolved model name.

---

## 2. Scope

### ✅ Priority 1 — Claude tier mapping (core deliverable)

Claude's `settings.json` already contains the *slots* for tier models; they
just need *different values*. Today's loop writes the same `model` into all
three:

```go
// internal/agent/claude.go:44-47
for _, tier := range []string{"OPUS", "SONNET", "HAIKU"} {
    env["ANTHROPIC_DEFAULT_"+tier+"_MODEL"]     = model
    env["ANTHROPIC_DEFAULT_"+tier+"_MODEL_NAME"] = model
}
```

**Target:** OPUS / SONNET / HAIKU each resolve to a distinct upstream model
(e.g. opus→`claude-opus-4`, sonnet→`claude-sonnet-5`, haiku→`claude-haiku-4.5`)
chosen by tier-aware resolution.

### ✅ Priority 2 — OpenCode dual-role (bonus)

Make `small_model` participate in resolution via `PickCustomModel` instead of
being hardcoded.

**Target:**
```go
cfg["model"]      = bestModel              // primary — already resolved
cfg["small_model"] = providerID + "/" + PickCustomModel("opencode", upstreamModels)
```

### ❌ Out of scope (structurally single-model)

These agents have no tier/role concept in their config format. Documented as a
known limitation; revisit when the upstream product adds tiers.

- `codex` — single `model` field (TOML)
- `kimi` — single `model` field (TOML)
- `hermes` — single `model` field (YAML)
- `gemini` — single `model` field (`.env`)
- `openclaw` — multi-model catalog exists but primary is single; defer
- `openclaude` — single primary; defer

---

## 3. Architecture / Data Flow

### Today (single model)

```
shared.DefaultModels map[string]string
                         │
                         ▼
              model.ResolveModelForAgent(agent, default)
                         │
                         ▼
              processAgents()  →  writer.Configure(path, p, model)
```

### After (tier-aware)

```
shared.DefaultModels map[string]TierModels
  TierModels{ Default, Opus, Sonnet, Haiku }
                         │
                         ▼
         model.ResolveTierModels(agent, upstream, proxyMap)
                         │
                         ▼
              processAgents()  →  writer.ConfigureWithTier(path, p, tms)
```

### Key design decision

**Keep `ConfigWriter.Configure(path, p, model)` as-is** for single-model
agents (backward compat). Add an **optional interface** that tier-aware writers
implement:

```go
// TieredConfigWriter extends ConfigWriter with per-tier model support.
// A writer that implements this is called via ConfigureTiered() when the
// caller has tier data; otherwise Configure() is used as fallback.
type TieredConfigWriter interface {
    ConfigWriter
    ConfigureTiered(path string, p *proxy.Proxy, tiers map[string]string) error
}
```

The registry in `processAgents()` checks `if tw, ok := writer.(TieredConfigWriter); ok`
and dispatches accordingly. This is the smallest invasive change:

- Single-model writers (codex, kimi, hermes, gemini) **need zero changes**.
- `claudeWriter` and `openCodeWriter` implement `ConfigureTiered()`.
- `Configure()` remains on the base interface for callers that only have one
  model string (status commands, single-model probes).

---

## 4. File-by-File Change Plan

### 4.1 `internal/shared/defaults.go` — Tier data structure

- Add `TierModels` struct:
  ```go
  type TierModels struct {
      Default string `json:"default"` // fallback when no tier info
      Opus    string `json:"opus"`
      Sonnet  string `json:"sonnet"`
      Haiku   string `json:"haiku"`
  }
  ```
- Replace `DefaultModels map[string]string` with `DefaultModels map[string]TierModels`.
- Update `GetDefaultModel()` to return the `.Default` field (preserves callers).
- Add `GetDefaultTierModels(agent) (TierModels, bool)`.
- Migration: keep `map[string]string` entries where all three tier fields equal
  the single default (no behavior change for single-model agents).

### 4.2 `internal/agent/agent.go` — New interface

- Add `TieredConfigWriter` interface (see §3).
- Optionally expose a registry helper:
  ```go
  func (r *WriterRegistry) GetTiered(name string) TieredConfigWriter { ... }
  ```

### 4.3 `internal/agent/claude.go` — Implement tiered config

- Add `ConfigureTiered(path, p, tiers)` that iterates OPUS/SONNET/HAIKU and
  writes `tiers["opus"]`, `tiers["sonnet"]`, `tiers["haiku"]` respectively.
- If a tier value is empty, fall back to `.Default`.
- `cfg["model"]` (the overall default) = `.Default`.

### 4.4 `internal/agent/opencode.go` — Dual-role

- Add `ConfigureTiered(path, p, tiers)`.
- `model` = tiers[`opus`] (or `.Default`); `small_model` = tiers[`haiku`]
  (or a separate `small` role if one is added to `TierModels`).
- Remove hardcoded `small_model` value.

### 4.5 `internal/model/resolver.go` — Tier resolution

- Add `ResolveTierModels(agentName string, upstreamModels []string,
  proxyModelMap map[string]string) TierModels` — calls
  `ResolveModelForAgent()` once per tier using each tier's default model name.
- Existing `ResolveModelForAgent` and `ResolveAllModels` stay unchanged
  (backward compat for single-model display).

### 4.6 `cmd/runconfset.go` — Dispatch

- In `processAgents()`, after resolving tier models, dispatch:
  ```go
  tms := model.ResolveTierModels(name, upstreamModels, proxyModelMap)
  if tw, ok := writer.(TieredConfigWriter); ok {
      tiers := map[string]string{
          "default": tms.Default,
          "opus":    tms.Opus,
          "sonnet":  tms.Sonnet,
          "haiku":   tms.Haiku,
      }
      if err := tw.ConfigureTiered(cfgPath, p, tiers); err != nil { ... }
  } else {
      // existing single-model path
      if err := writer.Configure(cfgPath, p, tms.Default); err != nil { ... }
  }
  ```

---

## 5. Acceptance Criteria

1. **Claude:** After `agent-nexus configure`, `~/.claude/settings.json` contains
   three *different* values for `ANTHROPIC_DEFAULT_OPUS_MODEL`,
   `ANTHROPIC_DEFAULT_SONNET_MODEL`, `ANTHROPIC_DEFAULT_HAIKU_MODEL`, each
   mapping to the upstream model matched to that tier.
2. **OpenCode:** `small_model` is no longer hardcoded; it is resolved from
   upstream via `PickCustomModel`.
3. **Backward compat:** Running `configure` with a single-model agent (codex,
   kimi, hermes, gemini) produces the exact same config as before.
4. **No regressions:** `go test ./...` passes; `go vet ./...` clean.
5. **Discover/display:** `proxy discover` still shows the per-agent default
   correctly (single-model display unchanged).

---

## 6. Implementation Order (recommended)

| Step | File(s) | Notes |
|------|---------|-------|
| 1 | `shared/defaults.go` | Add `TierModels`, migrate the map, add getters |
| 2 | `agent/agent.go` | Add `TieredConfigWriter` interface |
| 3 | `model/resolver.go` | Add `ResolveTierModels` |
| 4 | `agent/claude.go` | Implement `ConfigureTiered` |
| 5 | `agent/opencode.go` | Implement `ConfigureTiered` |
| 6 | `cmd/runconfset.go` | Dispatch to tiered or single path |
| 7 | Tests + `go vet` + manual smoke-test Claude config | Verify ACs |

---

## 7. Context from Previous Session

This branch was created as a follow-up to two prior pieces of work:

- **Gemini 500 fix** (agent-proxy, merged & verified) — Gemini native endpoint
  places the model in the URL path, not the body. Fix used context propagation
  across 7 agent-proxy files + `MANUAL.md`. See commit `6790314`.
- **Tier-mapping research** — full per-agent capability matrix was produced.
  Only Claude has tier slots; OpenCode has dual roles; the rest are structurally
  single-model.

### Reference: the existing Claude tier loop (the primary target)

```
F:/src/agent-nexus/internal/agent/claude.go:44-47
```

### Reference: the ConfigWriter interface

```
F:/src/agent-nexus/internal/agent/agent.go:6-17
```

### Reference: the single default-model map

```
F:/src/agent-nexus/internal/shared/defaults.go:12-21
```

### Reference: OpenCode dual roles

```
F:/src/agent-nexus/internal/agent/opencode.go:54-55
```

---

## 8. Quick-Start for Next Agent

When an agent/IDE opens this branch:

1. Read this file.
2. Follow §6 Implementation Order.
3. Start a Go test session: `go test ./...` to confirm baseline before changes.
4. After each step, run `go vet ./...` and commit incrementally on the branch.
5. For Claude smoke-test: run `agent-nexus configure --agent claude --db <id>`,
   then inspect `~/.claude/settings.json` for three distinct tier values.
