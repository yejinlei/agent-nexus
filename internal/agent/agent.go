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

// WriterRegistry holds all config writers
type WriterRegistry struct {
	writers []ConfigWriter
}

func NewWriterRegistry() *WriterRegistry {
	return &WriterRegistry{
		writers: []ConfigWriter{
			// 原有可配置 Agent
			newCodexWriter(),
			newClaudeWriter(),
			newKimiWriter(),
			newDeepSeekWriter(),
			newOpenCodeWriter(),
			newOpenClawWriter(),
			newCursorWriter(),
			// 新增可配置 Agent
			newCodeBuddyWriter(),
			newHermesWriter(),
			newKiroWriter(),
			newGrokWriter(),
			newQoderWriter(),
			newTraeWriter(),

			// Pi (JSON-based config)
			newPiWriter(),
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


