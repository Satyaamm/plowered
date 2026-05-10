// Package dsr implements GDPR Article 15-20 Data Subject Requests:
// access, portability, rectification, erasure, and restriction. The Repo
// is the system of record; processing is asynchronous (a worker in the
// fullness of time builds the export bundle and writes the artifact_urn).
//
// The 30-day clock starts at receipt and is enforced by `due_at`. Late
// requests are visible to ops via List(...) where the due date precedes
// the current time.
//
// Schema lives in migration 0004_enterprise (see `dsr_requests`).
package dsr

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// ErrNotFound is returned by Get when an id is unknown.
var ErrNotFound = errors.New("dsr: not found")

// Type enumerates the GDPR Art. 15-20 right being exercised.
type Type string

const (
	TypeAccess         Type = "access"         // Art. 15 — right of access
	TypePortability    Type = "portability"    // Art. 20 — data portability
	TypeRectification  Type = "rectification"  // Art. 16 — correction
	TypeErasure        Type = "erasure"        // Art. 17 — right to be forgotten
	TypeRestriction    Type = "restriction"    // Art. 18 — restriction of processing
)

// Status moves received → processing → (completed | rejected).
type Status string

const (
	StatusReceived   Status = "received"
	StatusProcessing Status = "processing"
	StatusCompleted  Status = "completed"
	StatusRejected   Status = "rejected"
)

// Request mirrors a row in dsr_requests. The 30-day SLA lives in DueAt;
// callers MUST stamp that on receipt — do not derive it later from
// ReceivedAt because regulator audits look for the value as-of-receipt.
type Request struct {
	ID          string
	TenantID    string
	SubjectID   string // pseudonymous id for the data subject (email hash, customer ID)
	Type        Type
	Status      Status
	ReceivedAt  time.Time
	DueAt       time.Time // received + 30 days for GDPR
	CompletedAt time.Time
	HandledBy   string
	Notes       string
	ArtifactURN string // S3 path of the export bundle
}

// SLA is the regulator-mandated turnaround for GDPR DSRs. Stamped onto
// DueAt at receipt.
const SLA = 30 * 24 * time.Hour

// Repo persists and queries DSR records.
type Repo interface {
	Create(ctx context.Context, r *Request) (*Request, error)
	Get(ctx context.Context, tenantID, id string) (*Request, error)
	List(ctx context.Context, tenantID string) ([]*Request, error)
	UpdateStatus(ctx context.Context, tenantID, id string, status Status, handledBy, artifactURN, notes string) error
}

// MemoryRepo is the in-process implementation used for tests and dev
// mode while the user/tenant signup flow is still pending. It supports
// all four Repo methods on a tenant-keyed map.
type MemoryRepo struct {
	mu sync.RWMutex
	by map[string]map[string]*Request // tenant_id → id → request
}

func NewMemoryRepo() *MemoryRepo {
	return &MemoryRepo{by: make(map[string]map[string]*Request)}
}

func (m *MemoryRepo) Create(_ context.Context, r *Request) (*Request, error) {
	if r == nil {
		return nil, errors.New("dsr: nil request")
	}
	cp := *r
	if cp.ID == "" {
		cp.ID = newID()
	}
	if cp.ReceivedAt.IsZero() {
		cp.ReceivedAt = time.Now().UTC()
	}
	if cp.DueAt.IsZero() {
		cp.DueAt = cp.ReceivedAt.Add(SLA)
	}
	if cp.Status == "" {
		cp.Status = StatusReceived
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	bucket := m.by[cp.TenantID]
	if bucket == nil {
		bucket = make(map[string]*Request)
		m.by[cp.TenantID] = bucket
	}
	bucket[cp.ID] = &cp
	return &cp, nil
}

func (m *MemoryRepo) Get(_ context.Context, tenantID, id string) (*Request, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if r, ok := m.by[tenantID][id]; ok {
		cp := *r
		return &cp, nil
	}
	return nil, ErrNotFound
}

func (m *MemoryRepo) List(_ context.Context, tenantID string) ([]*Request, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	src := m.by[tenantID]
	out := make([]*Request, 0, len(src))
	for _, r := range src {
		cp := *r
		out = append(out, &cp)
	}
	return out, nil
}

func (m *MemoryRepo) UpdateStatus(_ context.Context, tenantID, id string, status Status, handledBy, artifactURN, notes string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.by[tenantID][id]
	if !ok {
		return ErrNotFound
	}
	r.Status = status
	if handledBy != "" {
		r.HandledBy = handledBy
	}
	if artifactURN != "" {
		r.ArtifactURN = artifactURN
	}
	if notes != "" {
		r.Notes = notes
	}
	if status == StatusCompleted && r.CompletedAt.IsZero() {
		r.CompletedAt = time.Now().UTC()
	}
	return nil
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "dsr-fallback"
	}
	return hex.EncodeToString(b[:])
}
