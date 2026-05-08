package llm

import (
	"fmt"
	"sort"
	"sync"
)

// Factory builds a Provider from configuration. It is called once per
// tenant×provider combination at server boot or on hot reload.
type Factory func(cfg map[string]any) (Provider, error)

// Registry maps provider name → factory. The package-level Default is
// populated by sub-package init() functions so the server can list available
// providers without importing each one.
type Registry struct {
	mu        sync.RWMutex
	factories map[string]Factory
}

func NewRegistry() *Registry {
	return &Registry{factories: make(map[string]Factory)}
}

var Default = NewRegistry()

func (r *Registry) Register(name string, f Factory) error {
	if name == "" || f == nil {
		return fmt.Errorf("llm registry: name and factory required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.factories[name]; exists {
		return fmt.Errorf("llm registry: %q already registered", name)
	}
	r.factories[name] = f
	return nil
}

func (r *Registry) MustRegister(name string, f Factory) {
	if err := r.Register(name, f); err != nil {
		panic(err)
	}
}

func (r *Registry) Build(name string, cfg map[string]any) (Provider, error) {
	r.mu.RLock()
	f, ok := r.factories[name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("llm registry: unknown provider %q", name)
	}
	return f(cfg)
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.factories))
	for k := range r.factories {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// Router selects a Provider per request based on simple rules: a tenant
// override, an agent override, then a default. Failover to a fallback chain
// is layered on top via Generate's retry loop.
type Router struct {
	Default      Provider
	Fallbacks    []Provider
	TenantOverrides map[string]Provider // tenantID → provider
	AgentOverrides  map[string]Provider // agentName → provider
}

// PickFor returns the provider chain (primary first, fallbacks after) for a
// given (tenant, agent) pair.
func (r Router) PickFor(tenant, agent string) []Provider {
	primary := r.Default
	if p, ok := r.TenantOverrides[tenant]; ok {
		primary = p
	}
	if p, ok := r.AgentOverrides[agent]; ok {
		primary = p
	}
	if primary == nil {
		return r.Fallbacks
	}
	out := make([]Provider, 0, 1+len(r.Fallbacks))
	out = append(out, primary)
	out = append(out, r.Fallbacks...)
	return out
}
