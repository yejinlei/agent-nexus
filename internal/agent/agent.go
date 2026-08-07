package agent

import "agent-nexus/internal/proxy"

// ConfigWriter can apply proxy configuration to an agent
type ConfigWriter interface {
	Name() string
	Category() string
	CanConfigure(p *proxy.Proxy) bool
	// Configure writes the proxy config. If model is non-empty, the agent
	// will be written with that model name instead of its default.
	Configure(path string, p *proxy.Proxy, model string) error
	// Status reports whether the agent is configured.
	Status(path string) (bool, string)
	// StatusModel reports configured model name, source, and notes.
	StatusModel(path string) (model, source, notes string)
}

// TieredConfigWriter extends ConfigWriter with per-tier model support.
// A writer that implements this is called with named role→model mappings
// (opus/sonnet/haiku) instead of a single model string. Writers whose config
// format has no tier concept (codex, kimi, hermes, gemini) simply do not
// implement this interface and continue using Configure().
type TieredConfigWriter interface {
	ConfigWriter
	// ConfigureTiered writes the proxy config with per-tier model names.
	// tiers["default"] is the fallback; tiers["opus"] / ["sonnet"] / ["haiku"]
	// are the named roles. An empty role value means the caller should use
	// tiers["default"] for that slot.
	ConfigureTiered(path string, p *proxy.Proxy, tiers map[string]string) error
}

// Resetter can remove all agent-nexus injected configuration, restoring the
// agent to its original (pre-configure) state. Writers that only add whole
// files simply delete them; writers that merge into existing files perform
// surgical removal of the keys they own and return any auxiliary files
// (e.g. auth.json) for the caller to delete.
type Resetter interface {
	// Reset restores the agent to its original state.
	// path is the primary config file path (from discovery).
	// Returns the list of auxiliary files to delete (empty if none).
	Reset(path string) ([]string, error)
}

// HasResetter checks whether a writer implements the Resetter interface.
func HasResetter(w ConfigWriter) bool {
	_, ok := w.(Resetter)
	return ok
}

// WriterRegistry holds all config writers
type WriterRegistry struct {
	writers []ConfigWriter
}

func NewWriterRegistry() *WriterRegistry {
	return &WriterRegistry{
		writers: []ConfigWriter{
			// 可配置 Agent
			newCodexWriter(),
			newClaudeWriter(),
			newKimiWriter(),
			newOpenCodeWriter(),
			newOpenClawWriter(),
			newHermesWriter(),
			newGeminiWriter(),

			// OpenClaude
			newOpenClaudeWriter(),
		},
	}
}

func (r *WriterRegistry) Get(name string) ConfigWriter {
	for _, w := range r.writers {
		if w.Name() == name {
			return w
		}
	}
	return nil
}

func (r *WriterRegistry) All() []ConfigWriter {
	return r.writers
}
