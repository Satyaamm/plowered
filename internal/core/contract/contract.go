// Package contract implements data contracts: producer-declared
// guarantees on a catalogued asset (expected schema, freshness budget,
// per-column null thresholds) that the platform continuously validates
// against the asset's profile, surfacing breaches as Events that route
// through the notify dispatcher.
//
// Design choices:
//
//   - One active contract per (tenant, asset). Updating creates a new
//     version on the same row rather than a new row — the audit trail
//     lives on breach records, each carrying the contract_version it
//     was evaluated against.
//   - Evaluation is read-only. The evaluator pulls the latest profile
//     (already cached) and compares; no fresh source queries are
//     issued. Breaches are recorded even when no notify rule matches,
//     so the UI history view always has data.
//   - Breach kinds are a closed enum: schema_drift, freshness,
//     null_threshold. Adding a new kind means adding a comparator +
//     a UI badge — no schema change.
package contract

import (
	"context"
	"errors"
	"time"
)

// Status reflects the contract's lifecycle. Suspended contracts
// remain stored but the evaluator skips them; deprecated contracts
// are tombstoned (kept for audit, never re-checked, hidden from the
// default UI list).
type Status string

const (
	StatusActive     Status = "active"
	StatusSuspended  Status = "suspended"
	StatusDeprecated Status = "deprecated"
)

// BreachKind enumerates the violation categories the v0 evaluator
// surfaces.
type BreachKind string

const (
	BreachSchemaDrift   BreachKind = "schema_drift"
	BreachFreshness     BreachKind = "freshness"
	BreachNullThreshold BreachKind = "null_threshold"
)

// ExpectedColumn is one entry in the contract's expected_columns
// array. Type is optional — a contract may pin only the column set
// (drift detection only) or also the types.
type ExpectedColumn struct {
	Name string `json:"name"`
	Type string `json:"type,omitempty"`
}

// Contract is the producer's declaration.
type Contract struct {
	ID                string             `json:"id"`
	TenantID          string             `json:"tenant_id"`
	AssetID           string             `json:"asset_id"`
	OwnerID           string             `json:"owner_id,omitempty"`
	Status            Status             `json:"status"`
	Version           int                `json:"version"`
	ExpectedColumns   []ExpectedColumn   `json:"expected_columns,omitempty"`
	FreshnessSeconds  int                `json:"freshness_seconds,omitempty"`
	NullThresholds    map[string]float64 `json:"null_thresholds,omitempty"`
	Description       string             `json:"description,omitempty"`
	CreatedAt         time.Time          `json:"created_at"`
	UpdatedAt         time.Time          `json:"updated_at"`
}

// Breach is one detected violation. Observed + Expected are JSON
// payloads whose shape depends on Kind:
//
//   - schema_drift:   observed = {extra_columns: [], missing_columns: [], type_changes: {}}
//                     expected = {columns: [{name, type}, ...]}
//   - freshness:      observed = {profile_generated_at, age_seconds}
//                     expected = {max_age_seconds}
//   - null_threshold: observed = {column, null_fraction}
//                     expected = {column, max_null_fraction}
type Breach struct {
	ID              string         `json:"id"`
	TenantID        string         `json:"tenant_id"`
	ContractID      string         `json:"contract_id"`
	AssetID         string         `json:"asset_id"`
	ContractVersion int            `json:"contract_version"`
	Kind            BreachKind     `json:"kind"`
	Severity        string         `json:"severity"`
	Observed        map[string]any `json:"observed,omitempty"`
	Expected        map[string]any `json:"expected,omitempty"`
	Message         string         `json:"message,omitempty"`
	ObservedAt      time.Time      `json:"observed_at"`
}

// Store persists contracts + breaches.
type Store interface {
	UpsertContract(ctx context.Context, c *Contract) (*Contract, error)
	GetContract(ctx context.Context, tenantID, id string) (*Contract, error)
	GetContractByAsset(ctx context.Context, tenantID, assetID string) (*Contract, error)
	ListContracts(ctx context.Context, tenantID string) ([]*Contract, error)
	DeleteContract(ctx context.Context, tenantID, id string) error

	RecordBreach(ctx context.Context, b *Breach) (*Breach, error)
	ListBreaches(ctx context.Context, tenantID string, limit int) ([]*Breach, error)
	ListBreachesForContract(ctx context.Context, tenantID, contractID string, limit int) ([]*Breach, error)
}

// ErrNotFound is the canonical "no row" sentinel.
var ErrNotFound = errors.New("contract: not found")
