// Package cost is the unified cost-tracking surface for the platform.
// Every billable operation (AI completion, warehouse query, future
// items) writes one Record through this package, and the dashboard
// reads aggregates back through the same shape.
//
// Why one shared shape: operators want a single answer to "what is
// this tenant spending". Splitting by kind in the data layer would
// force a UNION across N tables every time the dashboard renders.
//
// The Recorder is the write surface used by call-sites that produce
// cost. The Reader is the read surface used by the API + dashboard.
package cost

import (
	"context"
	"time"
)

// Kind names the category of operation that produced the cost. Keep
// the set small; per-kind sub-types live in Attributes.
type Kind string

const (
	KindAICompletion   Kind = "ai_completion"
	KindWarehouseQuery Kind = "warehouse_query"
)

// Record is one line item.
type Record struct {
	ID         string         `json:"id"`
	TenantID   string         `json:"tenant_id"`
	TS         time.Time      `json:"ts"`
	Kind       Kind           `json:"kind"`
	Provider   string         `json:"provider"`
	CostUSD    float64        `json:"cost_usd"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

// DailyTotal aggregates cost over one calendar day, partitioned by
// (kind, provider). Used by the dashboard.
type DailyTotal struct {
	Day      time.Time `json:"day"`
	Kind     Kind      `json:"kind"`
	Provider string    `json:"provider"`
	CostUSD  float64   `json:"cost_usd"`
	Count    int64     `json:"count"`
}

// Recorder is the write surface. Implementations are non-blocking on
// the happy path — call-sites are on the request hot path and should
// not wait on Postgres for cost ledger writes.
type Recorder interface {
	Record(ctx context.Context, r Record) error
}

// Reader is the read surface used by the API.
type Reader interface {
	// Recent returns the latest N records for a tenant, newest first.
	Recent(ctx context.Context, tenantID string, limit int) ([]*Record, error)
	// Daily returns per-day totals split by (kind, provider) over the
	// inclusive [from, to] range. Days with zero cost are omitted.
	Daily(ctx context.Context, tenantID string, from, to time.Time) ([]*DailyTotal, error)
}

// Store is the union; the postgres impl satisfies both.
type Store interface {
	Recorder
	Reader
	BudgetStore
}

// Budget is the per-tenant monthly cap. Nil MonthlyUSD = disabled.
type Budget struct {
	TenantID     string     `json:"tenant_id"`
	MonthlyUSD   *float64   `json:"monthly_usd,omitempty"`
	WarnAtPct    int        `json:"warn_at_pct"`
	HardAtPct    int        `json:"hard_at_pct"`
	LastWarnedAt *time.Time `json:"last_warned_at,omitempty"`
	LastHardAt   *time.Time `json:"last_hard_at,omitempty"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// BudgetStore is the per-tenant budget surface.
type BudgetStore interface {
	GetBudget(ctx context.Context, tenantID string) (*Budget, error)
	UpsertBudget(ctx context.Context, b *Budget) (*Budget, error)
	MarkWarned(ctx context.Context, tenantID string, at time.Time) error
	MarkHard(ctx context.Context, tenantID string, at time.Time) error
	// RollingTotal returns the sum of cost_usd over the last 30 days
	// (or other window if Days is non-zero). Used by the Watcher.
	RollingTotal(ctx context.Context, tenantID string, days int) (float64, error)
}

// NoopRecorder drops every record. Use when cost-tracking is disabled
// or the storage backend isn't available (in-memory dev mode).
type NoopRecorder struct{}

func (NoopRecorder) Record(_ context.Context, _ Record) error { return nil }
