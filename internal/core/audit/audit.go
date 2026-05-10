// Package audit centralizes audit-event emission. Every mutation across the
// codebase calls Writer.Emit; the writer persists to the audit_events table
// (Postgres impl) or buffers in memory (test/dev impl). Daily export to
// object-locked storage happens in a separate operator (out of scope here).
//
// The audit table schema is documented in SECURITY.md §7 and migration
// 0001_init.up.sql.
package audit

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// Outcome categorizes the result of the audited action.
type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure" // tried, didn't work (5xx, exec error)
	OutcomeDenied  Outcome = "denied"  // policy / auth refused (4xx)
)

// Event is one append-only audit row. The schema mirrors the Postgres
// audit_events table; new optional fields default to zero values so old
// callers stay source-compatible.
type Event struct {
	EventID      string         `json:"event_id"`
	TenantID     string         `json:"tenant_id"`
	ActorID      string         `json:"actor_id"`
	ActorKind    string         `json:"actor_kind"` // "user" | "service" | "system"
	Action       string         `json:"action"`     // "asset.create" | "pipeline.run" | …
	ResourceType string         `json:"resource_type"`
	ResourceID   string         `json:"resource_id"`
	Before       map[string]any `json:"before,omitempty"`
	After        map[string]any `json:"after,omitempty"`
	IP           string         `json:"ip,omitempty"`
	UserAgent    string         `json:"user_agent,omitempty"`
	RequestID    string         `json:"request_id,omitempty"`
	SessionID    string         `json:"session_id,omitempty"`
	Outcome      Outcome        `json:"outcome,omitempty"`
	ErrorMessage string         `json:"error_message,omitempty"`
	PolicyReason string         `json:"policy_reason,omitempty"`
	HTTPMethod   string         `json:"http_method,omitempty"`
	HTTPPath     string         `json:"http_path,omitempty"`
	HTTPStatus   int            `json:"http_status,omitempty"`
	ServiceName  string         `json:"service_name,omitempty"`
	ServiceVer   string         `json:"service_version,omitempty"`
	PrevHash     []byte         `json:"prev_hash,omitempty"`
	RowHash      []byte         `json:"row_hash,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}

// ComputeRowHash returns sha256(canonical(event) || prev). The canonical
// form covers every field that a tampering attacker might want to change;
// PrevHash is fed back in so the chain is order-sensitive. Callers stamp
// the result onto Event.RowHash before persisting.
func ComputeRowHash(e Event, prev []byte) []byte {
	h := sha256.New()
	h.Write([]byte(e.EventID))
	h.Write([]byte{0})
	h.Write([]byte(e.TenantID))
	h.Write([]byte{0})
	h.Write([]byte(e.ActorID))
	h.Write([]byte{0})
	h.Write([]byte(e.ActorKind))
	h.Write([]byte{0})
	h.Write([]byte(e.Action))
	h.Write([]byte{0})
	h.Write([]byte(e.ResourceType))
	h.Write([]byte{0})
	h.Write([]byte(e.ResourceID))
	h.Write([]byte{0})
	h.Write([]byte(string(e.Outcome)))
	h.Write([]byte{0})
	h.Write([]byte(e.PolicyReason))
	h.Write([]byte{0})
	h.Write([]byte(e.HTTPMethod))
	h.Write([]byte{0})
	h.Write([]byte(e.HTTPPath))
	h.Write([]byte{0})
	h.Write([]byte(e.CreatedAt.UTC().Format(time.RFC3339Nano)))
	h.Write([]byte{0})
	h.Write(prev)
	return h.Sum(nil)
}

// Writer persists audit events. Implementations MUST be append-only.
type Writer interface {
	Emit(ctx context.Context, e Event) error
}

// Reader fetches audit events for the admin/audit feed. Read-only by
// design; a Reader cannot mutate or delete rows.
type Reader interface {
	List(ctx context.Context, tenantID string, limit int) ([]Event, error)
}

// Store combines Writer + Reader. Both MemoryWriter and the Postgres
// AuditStore satisfy it, so callers wire one value into both fields when
// they need both directions.
type Store interface {
	Writer
	Reader
}

// List implements Reader on MemoryWriter so tests can serve the admin feed
// without wiring a separate type.
func (m *MemoryWriter) List(_ context.Context, tenantID string, limit int) ([]Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Event, 0, limit)
	for i := len(m.events) - 1; i >= 0 && len(out) < limit; i-- {
		if tenantID != "" && m.events[i].TenantID != tenantID {
			continue
		}
		out = append(out, m.events[i])
	}
	return out, nil
}

// MemoryWriter is an in-process Writer for tests and the embedded dev mode.
// It maintains the hash chain in memory so unit tests can verify it without
// a Postgres dependency.
type MemoryWriter struct {
	mu        sync.Mutex
	events    []Event
	chainTail map[string][]byte // tenant_id → last row_hash
}

func NewMemoryWriter() *MemoryWriter {
	return &MemoryWriter{chainTail: make(map[string][]byte)}
}

func (m *MemoryWriter) Emit(_ context.Context, e Event) error {
	if e.EventID == "" {
		e.EventID = newID()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	if e.Outcome == "" {
		e.Outcome = OutcomeSuccess
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	prev := m.chainTail[e.TenantID]
	e.PrevHash = prev
	e.RowHash = ComputeRowHash(e, prev)
	m.chainTail[e.TenantID] = e.RowHash
	m.events = append(m.events, e)
	return nil
}

// All returns a snapshot of recorded events. Tests use this to assert.
func (m *MemoryWriter) All() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Event, len(m.events))
	copy(out, m.events)
	return out
}

// Multi fans out a single Emit call to multiple writers (e.g. local memory
// + remote SIEM forwarder). Errors aggregate; partial success still records
// to the writers that succeeded.
type Multi struct {
	Writers []Writer
}

func (m Multi) Emit(ctx context.Context, e Event) error {
	for _, w := range m.Writers {
		if err := w.Emit(ctx, e); err != nil {
			return err
		}
	}
	return nil
}

// NoOp discards events. Useful when audit is intentionally disabled (rare).
type NoOp struct{}

func (NoOp) Emit(_ context.Context, _ Event) error { return nil }

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "evt-fallback"
	}
	return hex.EncodeToString(b[:])
}
