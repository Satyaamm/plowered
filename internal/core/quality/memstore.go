package quality

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// ErrNotFound is returned when a Check or CheckRun id is unknown.
var ErrNotFound = errors.New("quality: not found")

// Store is the persistence interface for Checks and their CheckRuns.
type Store interface {
	CreateCheck(ctx context.Context, c *Check) (*Check, error)
	UpdateCheck(ctx context.Context, c *Check) (*Check, error)
	DeleteCheck(ctx context.Context, tenantID, id string) error
	GetCheck(ctx context.Context, id string) (*Check, error)
	ListChecks(ctx context.Context, tenantID, assetID string) ([]*Check, error)

	RecordRun(ctx context.Context, run *CheckRun) (*CheckRun, error)
	ListRuns(ctx context.Context, tenantID, checkID string, limit int) ([]*CheckRun, error)
}

// MemoryStore is an in-process Store for tests + embedded mode.
type MemoryStore struct {
	mu     sync.RWMutex
	checks map[string]*Check
	runs   map[string]*CheckRun
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		checks: make(map[string]*Check),
		runs:   make(map[string]*CheckRun),
	}
}

func (m *MemoryStore) CreateCheck(_ context.Context, c *Check) (*Check, error) {
	if c == nil {
		return nil, errors.New("quality: nil check")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if c.ID == "" {
		c.ID = newID()
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	c.UpdatedAt = c.CreatedAt
	cp := *c
	m.checks[c.ID] = &cp
	return &cp, nil
}

func (m *MemoryStore) UpdateCheck(_ context.Context, c *Check) (*Check, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.checks[c.ID]; !ok {
		return nil, ErrNotFound
	}
	c.UpdatedAt = time.Now().UTC()
	cp := *c
	m.checks[c.ID] = &cp
	return &cp, nil
}

func (m *MemoryStore) DeleteCheck(_ context.Context, tenantID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.checks[id]
	if !ok || (tenantID != "" && c.TenantID != tenantID) {
		return ErrNotFound
	}
	delete(m.checks, id)
	return nil
}

func (m *MemoryStore) GetCheck(_ context.Context, id string) (*Check, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	c, ok := m.checks[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *c
	return &cp, nil
}

func (m *MemoryStore) ListChecks(_ context.Context, tenantID, assetID string) ([]*Check, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Check, 0, len(m.checks))
	for _, c := range m.checks {
		if c.TenantID != tenantID {
			continue
		}
		if assetID != "" && c.AssetID != assetID {
			continue
		}
		cp := *c
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

func (m *MemoryStore) RecordRun(_ context.Context, r *CheckRun) (*CheckRun, error) {
	if r == nil {
		return nil, errors.New("quality: nil run")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.ID == "" {
		r.ID = newID()
	}
	cp := *r
	m.runs[r.ID] = &cp
	return &cp, nil
}

func (m *MemoryStore) ListRuns(_ context.Context, tenantID, checkID string, limit int) ([]*CheckRun, error) {
	if limit <= 0 {
		limit = 50
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*CheckRun, 0, limit)
	for _, r := range m.runs {
		if r.TenantID != tenantID {
			continue
		}
		if checkID != "" && r.CheckID != checkID {
			continue
		}
		cp := *r
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
