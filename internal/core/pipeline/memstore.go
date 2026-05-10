package pipeline

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

// ErrNotFound is returned by MemoryStore lookups when an ID is unknown.
var ErrNotFound = errors.New("pipeline: not found")

// Repo is the surface the HTTP layer needs from a pipeline store. Both
// MemoryStore and the Postgres adapter satisfy it.
type Repo interface {
	Store // GetPipeline, GetRun, UpdateRun, CreateTaskRun, UpdateTaskRun, ListTaskRuns

	CreatePipeline(ctx context.Context, p *Pipeline) (*Pipeline, error)
	UpdatePipeline(ctx context.Context, p *Pipeline) (*Pipeline, error)
	DeletePipeline(ctx context.Context, tenantID, id string) error
	ListPipelines(ctx context.Context, tenantID string) ([]*Pipeline, error)

	CreateRun(ctx context.Context, r *Run) (*Run, error)
	ListRuns(ctx context.Context, tenantID, pipelineID string, limit int) ([]*Run, error)

	// Scheduler / reaper surface — used by internal/scheduler. tenantID == ""
	// matches all tenants (the scheduler runs as a system actor).
	ListSchedulablePipelines(ctx context.Context, tenantID string) ([]*Pipeline, error)
	ListStuckRuns(ctx context.Context, olderThan time.Time) ([]*Run, error)
	HeartbeatRun(ctx context.Context, runID string, at time.Time) error
}

// MemoryStore is an in-process implementation of Store for tests and the
// embedded dev mode. It additionally exposes Create/List methods consumed
// by the HTTP layer.
type MemoryStore struct {
	mu        sync.RWMutex
	pipelines map[string]*Pipeline
	runs      map[string]*Run
	taskRuns  map[string]*TaskRun
}

// NewMemoryStore builds an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		pipelines: make(map[string]*Pipeline),
		runs:      make(map[string]*Run),
		taskRuns:  make(map[string]*TaskRun),
	}
}

// CreatePipeline persists a new Pipeline. ID is assigned if blank.
func (m *MemoryStore) CreatePipeline(_ context.Context, p *Pipeline) (*Pipeline, error) {
	if p == nil {
		return nil, errors.New("pipeline: nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if p.ID == "" {
		p.ID = newID()
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now().UTC()
	}
	p.UpdatedAt = p.CreatedAt
	cp := *p
	m.pipelines[p.ID] = &cp
	return &cp, nil
}

// UpdatePipeline replaces an existing Pipeline.
func (m *MemoryStore) UpdatePipeline(_ context.Context, p *Pipeline) (*Pipeline, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.pipelines[p.ID]; !ok {
		return nil, ErrNotFound
	}
	p.UpdatedAt = time.Now().UTC()
	cp := *p
	m.pipelines[p.ID] = &cp
	return &cp, nil
}

// DeletePipeline removes a Pipeline; runs are kept for audit.
func (m *MemoryStore) DeletePipeline(_ context.Context, tenantID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.pipelines[id]
	if !ok || (tenantID != "" && p.TenantID != tenantID) {
		return ErrNotFound
	}
	delete(m.pipelines, id)
	return nil
}

// ListPipelines returns pipelines for a tenant, oldest first.
func (m *MemoryStore) ListPipelines(_ context.Context, tenantID string) ([]*Pipeline, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Pipeline, 0, len(m.pipelines))
	for _, p := range m.pipelines {
		if p.TenantID == tenantID {
			cp := *p
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// GetPipeline implements Store.
func (m *MemoryStore) GetPipeline(_ context.Context, id string) (*Pipeline, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.pipelines[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *p
	return &cp, nil
}

// CreateRun persists a Run; ID is assigned if blank.
func (m *MemoryStore) CreateRun(_ context.Context, r *Run) (*Run, error) {
	if r == nil {
		return nil, errors.New("run: nil")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.ID == "" {
		r.ID = newID()
	}
	if r.Status == "" {
		r.Status = RunQueued
	}
	if r.ScheduledAt.IsZero() {
		r.ScheduledAt = time.Now().UTC()
	}
	cp := *r
	m.runs[r.ID] = &cp
	return &cp, nil
}

// GetRun implements Store.
func (m *MemoryStore) GetRun(_ context.Context, id string) (*Run, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.runs[id]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *r
	return &cp, nil
}

// UpdateRun implements Store.
func (m *MemoryStore) UpdateRun(_ context.Context, r *Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.runs[r.ID]; !ok {
		return ErrNotFound
	}
	cp := *r
	m.runs[r.ID] = &cp
	return nil
}

// ListRuns returns runs for a tenant. If pipelineID is set, filters to that
// pipeline. Newest first; capped at limit (0 → 50).
func (m *MemoryStore) ListRuns(_ context.Context, tenantID, pipelineID string, limit int) ([]*Run, error) {
	if limit <= 0 {
		limit = 50
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Run, 0, limit)
	for _, r := range m.runs {
		if r.TenantID != tenantID {
			continue
		}
		if pipelineID != "" && r.PipelineID != pipelineID {
			continue
		}
		cp := *r
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ScheduledAt.After(out[j].ScheduledAt)
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// CreateTaskRun implements Store.
func (m *MemoryStore) CreateTaskRun(_ context.Context, tr *TaskRun) (*TaskRun, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if tr.ID == "" {
		tr.ID = newID()
	}
	cp := *tr
	m.taskRuns[tr.ID] = &cp
	return &cp, nil
}

// UpdateTaskRun implements Store.
func (m *MemoryStore) UpdateTaskRun(_ context.Context, tr *TaskRun) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.taskRuns[tr.ID]; !ok {
		return ErrNotFound
	}
	cp := *tr
	m.taskRuns[tr.ID] = &cp
	return nil
}

// ListTaskRuns implements Store. Returns task runs ordered by StartedAt.
func (m *MemoryStore) ListTaskRuns(_ context.Context, runID string) ([]*TaskRun, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*TaskRun, 0)
	for _, tr := range m.taskRuns {
		if tr.RunID == runID {
			cp := *tr
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out, nil
}

// ListSchedulablePipelines returns pipelines whose Schedule is non-nil and
// Enabled. tenantID == "" matches all tenants.
func (m *MemoryStore) ListSchedulablePipelines(_ context.Context, tenantID string) ([]*Pipeline, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Pipeline, 0)
	for _, p := range m.pipelines {
		if tenantID != "" && p.TenantID != tenantID {
			continue
		}
		if p.Schedule == nil || !p.Schedule.Enabled || p.Schedule.Cron == "" {
			continue
		}
		cp := *p
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// ListStuckRuns returns running runs whose heartbeat (or started_at when
// heartbeat is unset) is older than the given cutoff. Across all tenants.
func (m *MemoryStore) ListStuckRuns(_ context.Context, olderThan time.Time) ([]*Run, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Run, 0)
	for _, r := range m.runs {
		if r.Status != RunRunning {
			continue
		}
		check := r.LastHeartbeat
		if check.IsZero() {
			check = r.StartedAt
		}
		if check.IsZero() || check.After(olderThan) {
			continue
		}
		cp := *r
		out = append(out, &cp)
	}
	return out, nil
}

// HeartbeatRun bumps last_heartbeat without touching status. Idempotent.
func (m *MemoryStore) HeartbeatRun(_ context.Context, runID string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.runs[runID]
	if !ok {
		return ErrNotFound
	}
	r.LastHeartbeat = at
	return nil
}
