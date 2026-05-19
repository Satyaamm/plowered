package migration

import (
	"context"
	"errors"
)

// ErrNotFound is the canonical "no row matched" sentinel.
var ErrNotFound = errors.New("migration: not found")

// Store is the persistence interface migrations.Service depends on.
// Implemented by internal/storage/postgres.MigrationStore in
// production and by an in-memory variant in tests.
type Store interface {
	// CreatePlan inserts and returns the persisted row (with ID +
	// timestamps populated by the store).
	CreatePlan(ctx context.Context, p *Plan) (*Plan, error)
	// GetPlan returns ErrNotFound when the plan doesn't exist in this
	// tenant.
	GetPlan(ctx context.Context, tenantID, planID string) (*Plan, error)
	// ListPlans returns every plan in the tenant, newest first.
	ListPlans(ctx context.Context, tenantID string) ([]*Plan, error)
	// DeletePlan is idempotent.
	DeletePlan(ctx context.Context, tenantID, planID string) error

	// StartRun inserts a new running-status row and returns it. Runs
	// are never updated to a different plan_id once created.
	StartRun(ctx context.Context, tenantID, planID string) (*Run, error)
	// FinishRun records the terminal state (Succeeded or Failed) plus
	// counters. The store is responsible for setting FinishedAt.
	FinishRun(ctx context.Context, tenantID, runID string, status RunStatus, rowsRead, rowsWritten int64, errStr string) error
	// ListRuns returns every run for a plan, newest first.
	ListRuns(ctx context.Context, tenantID, planID string) ([]*Run, error)
}
