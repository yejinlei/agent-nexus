package agent

import "agent-nexus/internal/proxy"

// ConfigWriter can apply proxy configuration to an agent
type ConfigWriter interface {
	Name() string
	Category() string
	CanConfigure(p *proxy.Proxy) bool
	Configure(path string, p *proxy.Proxy) error
	Status(path string) (bool, string)
}

// WriterRegistry holds all config writers
type WriterRegistry struct {
	writers []ConfigWriter
}

func NewWriterRegistry() *WriterRegistry {
	return &WriterRegistry{
		writers: []ConfigWriter{
			newCodexWriter(),
			newClaudeWriter(),
			newKimiWriter(),
			newDeepSeekWriter(),
			newOpenCodeWriter(),
			newOpenClawWriter(),
			newCursorWriter(),
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

