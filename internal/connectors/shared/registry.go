package shared

import (
	"fmt"
	"sort"
	"sync"
)

// Registry is a process-local map of connector name → factory. Concrete
// connector packages register themselves via init() so the API server can
// list available connectors without importing each one explicitly.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

// Factory builds a Connector instance from configuration. Factories must be
// idempotent and safe for concurrent calls.
type Factory func() Connector

func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]Factory)}
}

// Default is the package-level registry used by init() registrations.
var Default = NewRegistry()

func (r *Registry) Register(name string, f Factory) error {
	if name == "" || f == nil {
		return fmt.Errorf("registry: name and factory required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.factories[name]; exists {
		return fmt.Errorf("registry: %q already registered", name)
	}
	r.factories[name] = f
	return nil
}

// MustRegister panics on duplicate registration. Use only from init().
func (r *Registry) MustRegister(name string, f Factory) {
	if err := r.Register(name, f); err != nil {
		panic(err)
	}
}

// Build constructs a Connector by name, or returns an error if unknown.
func (r *Registry) Build(name string) (Connector, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	f, ok := r.factories[name]
	if !ok {
		return nil, fmt.Errorf("registry: unknown connector %q", name)
	}
	return f(), nil
}

// Names returns all registered connector names in lexical order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.factories))
	for k := range r.factories {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
