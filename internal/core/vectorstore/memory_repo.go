package vectorstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sort"
	"sync"
	"time"
)

// MemoryRepo is the in-process Repo for tests + memory-mode dev.
// Mirrors the ai_provider in-memory pattern.
type MemoryRepo struct {
	mu   sync.RWMutex
	rows map[string]*Config // id -> config
}

func NewMemoryRepo() *MemoryRepo {
	return &MemoryRepo{rows: make(map[string]*Config)}
}

func mid() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "vs-fallback"
	}
	return hex.EncodeToString(b[:])
}

func (m *MemoryRepo) Create(_ context.Context, c *Config) (*Config, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c.ID == "" {
		c.ID = mid()
	}
	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	cp := *c
	m.rows[c.ID] = &cp
	return &cp, nil
}

func (m *MemoryRepo) Get(_ context.Context, tenantID, id string) (*Config, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.rows[id]
	if !ok || c.TenantID != tenantID {
		return nil, ErrNotFound
	}
	cp := *c
	return &cp, nil
}

func (m *MemoryRepo) List(_ context.Context, tenantID string) ([]*Config, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Config, 0, len(m.rows))
	for _, c := range m.rows {
		if c.TenantID == tenantID {
			cp := *c
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (m *MemoryRepo) Update(_ context.Context, c *Config) (*Config, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.rows[c.ID]
	if !ok {
		return nil, ErrNotFound
	}
	existing.Name = c.Name
	existing.Endpoint = c.Endpoint
	existing.IndexName = c.IndexName
	existing.ClassName = c.ClassName
	existing.Collection = c.Collection
	existing.Dimension = c.Dimension
	existing.UpdatedAt = time.Now().UTC()
	cp := *existing
	return &cp, nil
}

func (m *MemoryRepo) Delete(_ context.Context, tenantID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.rows[id]
	if !ok || c.TenantID != tenantID {
		return ErrNotFound
	}
	delete(m.rows, id)
	return nil
}

func (m *MemoryRepo) MarkTested(_ context.Context, _, id string, ok bool, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, exists := m.rows[id]
	if !exists {
		return ErrNotFound
	}
	c.LastTestedAt = time.Now().UTC()
	c.LastTestOK = ok
	c.LastTestErr = errMsg
	return nil
}

func (m *MemoryRepo) SetPrimary(_ context.Context, tenantID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[id]; !ok {
		return ErrNotFound
	}
	for _, c := range m.rows {
		if c.TenantID == tenantID {
			c.IsPrimary = c.ID == id
		}
	}
	return nil
}

func (m *MemoryRepo) Primary(_ context.Context, tenantID string) (*Config, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, c := range m.rows {
		if c.TenantID == tenantID && c.IsPrimary {
			cp := *c
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

func (m *MemoryRepo) SetSecretURN(_ context.Context, tenantID, id, urn string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.rows[id]
	if !ok || c.TenantID != tenantID {
		return ErrNotFound
	}
	c.SecretURN = urn
	return nil
}

var _ Repo = (*MemoryRepo)(nil)
