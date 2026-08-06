# CLAUDE.md — agent-nexus

Active work is tracked in `docs/`. Read the relevant brief before making
changes.

## Currently Active Feature

- [Tier-based model mapping (Claude + OpenCode)](docs/FEATURE_TIER_MODEL_MAPPING.md)
  — branch `feat/tier-model-mapping`. See §8 Quick-Start in that file for how
  to pick up work.

## Repo Basics

- **Language:** Go
- **CLI name:** `agent-nexus`
- **Build:** `go build ./...` · `go vet ./...` · `go test ./...`
- **SQLite DB:** pure Go (`modernc.org/sqlite`); DB path defaults to
  `~/.agent-nexus/proxies.db`
- **Config writers:** `internal/agent/` — one file per agent, all implement the
  `ConfigWriter` interface in `internal/agent/agent.go`
- **Single source of truth for model names:** `internal/shared/defaults.go`

## Conventions

- Commit messages end with `Co-Authored-By: Claude <noreply@anthropic.com>`
- Keep interface surfaces small; prefer adding a secondary optional interface
  over changing an existing one (see `TieredConfigWriter` in the active feature).
- Backward compat with single-model agents is mandatory — do not change their
  behavior.
