// Package deleted is the recycle-bin: every DELETE on a domain row is
// captured here as a Record with the full payload, the principal who
// removed it, and the timestamp. Records persist indefinitely — only a
// super_admin can permanently purge.
//
// Restoring is just as cheap: read the payload, re-insert it on the
// source table, then mark the Record restored. Children that were
// cascade-deleted point at their parent via parent_tombstone_id so the
// API can offer a one-click "restore everything that came down with this".
package deleted

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"
)

// ErrNotFound is returned when an id is unknown or already purged.
var ErrNotFound = errors.New("deleted: not found")

// Reason categorizes why a row ended up in the recycle bin.
type Reason string

const (
	ReasonUserAction   Reason = "user_action"   // someone clicked Delete
	ReasonCascade      Reason = "cascade"       // parent was deleted
	ReasonGDPRErasure  Reason = "gdpr_erasure"  // Art. 17 right to be forgotten
	ReasonSystem       Reason = "system"        // automated cleanup (rare)
)

// Record captures one tombstoned row.
type Record struct {
	ID                string
	TenantID          string
	ResourceType      string         // "asset" | "pipeline" | "check" | …
	ResourceID        string         // the original row's id
	Payload           map[string]any // full row state at deletion
	DeletedBy         string
	DeletedKind       string // "user" | "service" | "system"
	DeletionReason    Reason
	RequestID         string
	ParentTombstoneID string

	DeletedAt   time.Time
	RestoredAt  time.Time
	RestoredBy  string
	PurgedAt    time.Time
	PurgedBy    string
}

// IsActive reports whether the tombstone is currently in the recycle bin
// (not restored, not purged).
func (r Record) IsActive() bool {
	return r.RestoredAt.IsZero() && r.PurgedAt.IsZero()
}

// Repo is the persistence surface. Implementations live in
// internal/core/deleted (memory) and internal/storage/postgres (production).
type Repo interface {
	Capture(ctx context.Context, r *Record) (*Record, error)
	Get(ctx context.Context, tenantID, id string) (*Record, error)
	List(ctx context.Context, tenantID string, opts ListOptions) ([]*Record, error)
	MarkRestored(ctx context.Context, tenantID, id, restoredBy string, at time.Time) error
	MarkPurged(ctx context.Context, tenantID, id, purgedBy string, at time.Time) error
}

// ListOptions controls the recycle-bin query. Limit defaults to 100; type
// "" matches every resource type. IncludeRestored / IncludePurged are off
// by default — the recycle bin shows only what's still recoverable.
type ListOptions struct {
	ResourceType    string
	Limit           int
	IncludeRestored bool
	IncludePurged   bool
}

// MemoryRepo is the in-process implementation for tests + embedded mode.
type MemoryRepo struct {
	mu      sync.RWMutex
	records map[string]*Record // id → record
}

func NewMemoryRepo() *MemoryRepo { return &MemoryRepo{records: make(map[string]*Record)} }

func (m *MemoryRepo) Capture(_ context.Context, r *Record) (*Record, error) {
	if r == nil {
		return nil, errors.New("deleted: nil record")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.ID == "" {
		r.ID = newID()
	}
	if r.DeletedAt.IsZero() {
		r.DeletedAt = time.Now().UTC()
	}
	if r.DeletionReason == "" {
		r.DeletionReason = ReasonUserAction
	}
	if r.DeletedKind == "" {
		r.DeletedKind = "user"
	}
	cp := *r
	m.records[r.ID] = &cp
	return &cp, nil
}

func (m *MemoryRepo) Get(_ context.Context, tenantID, id string) (*Record, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.records[id]
	if !ok || (tenantID != "" && r.TenantID != tenantID) {
		return nil, ErrNotFound
	}
	cp := *r
	return &cp, nil
}

func (m *MemoryRepo) List(_ context.Context, tenantID string, opts ListOptions) ([]*Record, error) {
	limit := opts.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Record, 0, limit)
	for _, r := range m.records {
		if r.TenantID != tenantID {
			continue
		}
		if opts.ResourceType != "" && r.ResourceType != opts.ResourceType {
			continue
		}
		if !opts.IncludeRestored && !r.RestoredAt.IsZero() {
			continue
		}
		if !opts.IncludePurged && !r.PurgedAt.IsZero() {
			continue
		}
		cp := *r
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeletedAt.After(out[j].DeletedAt) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *MemoryRepo) MarkRestored(_ context.Context, tenantID, id, restoredBy string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.records[id]
	if !ok || r.TenantID != tenantID {
		return ErrNotFound
	}
	if !r.PurgedAt.IsZero() {
		return errors.New("deleted: cannot restore a purged record")
	}
	r.RestoredAt = at
	r.RestoredBy = restoredBy
	return nil
}

func (m *MemoryRepo) MarkPurged(_ context.Context, tenantID, id, purgedBy string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.records[id]
	if !ok || r.TenantID != tenantID {
		return ErrNotFound
	}
	r.PurgedAt = at
	r.PurgedBy = purgedBy
	return nil
}

func newID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "del-fallback"
	}
	return hex.EncodeToString(b[:])
}
