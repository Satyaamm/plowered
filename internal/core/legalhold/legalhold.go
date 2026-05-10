// Package legalhold implements the GDPR-Art.5(1)(e) / SOC2-CC8 control
// that prevents a delete on resources caught by an active litigation
// hold. The Repo is the source of truth for active holds; Matches is the
// pure decision function the API layer calls before tombstoning.
//
// Schema lives in migration 0004_enterprise (see `legal_holds`).
//
// Scope JSON shape:
//
//	{
//	    "resource_types": ["pipeline", "check"],   // optional — match any of these types
//	    "resource_ids":   ["<uuid>", "<uuid>"],    // optional — exact id match
//	    "tags":           ["pii", "phi"]            // optional — only meaningful for assets
//	}
//
// A hold matches if **all** present clauses match. Empty clauses are
// ignored, so {"resource_types":["pipeline"]} catches every pipeline in
// the tenant.
package legalhold

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrHeld is returned by Repo.Check when an active hold blocks the
// delete. The handler maps it to HTTP 409 with the hold id so the user
// can ask Legal to release it.
var ErrHeld = errors.New("legalhold: resource is under an active legal hold")

// Hold mirrors a row in legal_holds. Only the fields the gate needs are
// surfaced — the full record (issuer, matter notes, audit) lives in
// Postgres and is read by the admin UI.
type Hold struct {
	ID         string
	TenantID   string
	Matter     string
	Reason     string
	Scope      Scope
	IssuedBy   string
	IssuedAt   time.Time
	ReleasedAt time.Time
}

// Scope is the structured form of legal_holds.scope JSONB.
type Scope struct {
	ResourceTypes []string `json:"resource_types,omitempty"`
	ResourceIDs   []string `json:"resource_ids,omitempty"`
	Tags          []string `json:"tags,omitempty"`
}

// IsActive reports whether the hold currently blocks deletion.
func (h Hold) IsActive() bool { return h.ReleasedAt.IsZero() }

// Matches reports whether this hold covers (resourceType, resourceID,
// tags). All non-empty scope clauses must match. An empty scope matches
// everything in the tenant — that is intentional, lets Legal apply a
// blanket hold during early discovery.
func (h Hold) Matches(resourceType, resourceID string, tags []string) bool {
	if !h.IsActive() {
		return false
	}
	if len(h.Scope.ResourceTypes) > 0 && !contains(h.Scope.ResourceTypes, resourceType) {
		return false
	}
	if len(h.Scope.ResourceIDs) > 0 && !contains(h.Scope.ResourceIDs, resourceID) {
		return false
	}
	if len(h.Scope.Tags) > 0 && !anyOverlap(h.Scope.Tags, tags) {
		return false
	}
	return true
}

// Repo persists and queries holds. The hot path is `Check`, called from
// every delete handler — implementations cache active holds in memory
// and refresh on Issue/Release so the gate adds nothing perceptible.
type Repo interface {
	List(ctx context.Context, tenantID string) ([]Hold, error)
	Issue(ctx context.Context, h *Hold) (*Hold, error)
	Release(ctx context.Context, tenantID, holdID, releasedBy string, at time.Time) error
	// Check returns ErrHeld with the matching Hold attached when the
	// resource is held. Returns nil when the delete is allowed.
	Check(ctx context.Context, tenantID, resourceType, resourceID string, tags []string) (*Hold, error)
}

// MemoryRepo is the in-process implementation used by tests and
// memory-backend dev mode.
type MemoryRepo struct {
	mu    sync.RWMutex
	holds map[string][]Hold // tenant_id → holds (active + released)
}

func NewMemoryRepo() *MemoryRepo {
	return &MemoryRepo{holds: make(map[string][]Hold)}
}

func (m *MemoryRepo) List(_ context.Context, tenantID string) ([]Hold, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	src := m.holds[tenantID]
	out := make([]Hold, len(src))
	copy(out, src)
	return out, nil
}

func (m *MemoryRepo) Issue(_ context.Context, h *Hold) (*Hold, error) {
	if h.ID == "" {
		h.ID = newID()
	}
	if h.IssuedAt.IsZero() {
		h.IssuedAt = time.Now().UTC()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.holds[h.TenantID] = append(m.holds[h.TenantID], *h)
	return h, nil
}

func (m *MemoryRepo) Release(_ context.Context, tenantID, holdID, releasedBy string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := m.holds[tenantID]
	for i := range list {
		if list[i].ID == holdID && list[i].IsActive() {
			list[i].ReleasedAt = at
			m.holds[tenantID] = list
			return nil
		}
	}
	return errors.New("legalhold: not found")
}

func (m *MemoryRepo) Check(_ context.Context, tenantID, resourceType, resourceID string, tags []string) (*Hold, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, h := range m.holds[tenantID] {
		if h.Matches(resourceType, resourceID, tags) {
			cp := h
			return &cp, ErrHeld
		}
	}
	return nil, nil
}

// NoopRepo returns "no holds" for every Check. Useful when wiring the
// gate is desirable but the schema isn't provisioned yet (early local dev).
type NoopRepo struct{}

func (NoopRepo) List(context.Context, string) ([]Hold, error) { return nil, nil }
func (NoopRepo) Issue(_ context.Context, h *Hold) (*Hold, error) {
	return h, errors.New("legalhold: noop repo cannot issue")
}
func (NoopRepo) Release(context.Context, string, string, string, time.Time) error {
	return errors.New("legalhold: noop repo cannot release")
}
func (NoopRepo) Check(context.Context, string, string, string, []string) (*Hold, error) {
	return nil, nil
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func anyOverlap(a, b []string) bool {
	for _, x := range a {
		if contains(b, x) {
			return true
		}
	}
	return false
}

func newID() string {
	var b [16]byte
	if _, err := randRead(b[:]); err != nil {
		return "hold-fallback"
	}
	return hexEncode(b[:])
}
