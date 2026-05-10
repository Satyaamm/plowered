// Package quality is the data-quality assertion engine. Checks are bound to
// assets, evaluated by a Runner, and produce CheckRun records that drive
// the asset-detail "trust" view and notification rules.
//
// See ORCHESTRATION.md §6 for the design.
package quality

import (
	"time"
)

// Check is one quality assertion bound to an asset.
type Check struct {
	ID            string
	TenantID      string
	AssetID       string         // foreign key to graph.Asset.id
	AssetQN       string         // denormalized for display
	Name          string         // human-readable
	Type          CheckType
	Config        map[string]any // type-specific
	Severity      Severity
	Owner         string
	Enabled       bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// CheckType discriminates check semantics. Built-in implementations live
// in this package; custom types can be registered against a custom Runner.
type CheckType string

const (
	CheckRowCount   CheckType = "row_count"
	CheckNotNull    CheckType = "not_null"
	CheckFreshness  CheckType = "freshness"
	CheckUniqueness CheckType = "uniqueness"
	CheckCustomSQL  CheckType = "custom_sql"
)

// Severity controls notification routing.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityError    Severity = "error"
	SeverityCritical Severity = "critical"
)

// Outcome enumerates CheckRun results.
type Outcome string

const (
	OutcomePass  Outcome = "pass"
	OutcomeFail  Outcome = "fail"
	OutcomeError Outcome = "error" // could not evaluate (DB error, bad config)
)

// CheckRun is one execution of a Check.
type CheckRun struct {
	ID         string
	TenantID   string
	CheckID    string
	AssetID    string
	Outcome    Outcome
	Value      float64        // measured value (row count, null count, age seconds, …)
	Threshold  float64        // asserted bound; semantics depend on Type
	Diagnostic string         // human-readable detail
	Properties map[string]any // type-specific extras (e.g. "duplicate_keys": [...])
	StartedAt  time.Time
	FinishedAt time.Time
	Duration   time.Duration
	Severity   Severity
}
