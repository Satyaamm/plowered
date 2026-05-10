// Package jobs is the visibility layer for long-running async work.
// Asynq (Redis Streams) owns the execution side; this package owns the
// durable per-tenant ledger that the UI polls and that survives worker
// restarts.
//
// The rhythm a handler follows:
//
//  1. Job := Repo.Create(type, resource_id)        // status = queued
//  2. enqueuer.Enqueue(...payload including job_id)
//  3. Return 202 {job_id} to the caller.
//
// The worker side:
//
//  1. Repo.Start(job_id)                            // status = running, started_at
//  2. Repo.Progress(job_id, pct, message)           // optional, called often
//  3. Repo.Succeed(job_id, resultJSON)              // status = succeeded
//     or Repo.Fail(job_id, errMessage)              // status = failed
package jobs

import (
	"context"
	"errors"
	"time"
)

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// IsTerminal reports whether the job has reached an end state.
func (s Status) IsTerminal() bool {
	switch s {
	case StatusSucceeded, StatusFailed, StatusCancelled:
		return true
	}
	return false
}

// Type names the canonical job classes the platform exposes. Add a
// const here when you ship a new background task class so the UI / API
// can render it.
const (
	TypeClassifyConnection = "classify:connection"
	TypeSearchReindex      = "search:reindex"
)

// Job is one durable async-work record. Result and ErrorMessage are
// populated when status is terminal.
type Job struct {
	ID          string
	TenantID    string
	Type        string
	Status      Status
	ProgressPct int
	Message     string
	Result      map[string]any
	ErrorMsg    string
	ActorID     string
	ResourceID  string
	CreatedAt   time.Time
	StartedAt   time.Time
	FinishedAt  time.Time
}

// Repo is the persistence interface. Postgres impl lives in
// internal/storage/postgres.
type Repo interface {
	Create(ctx context.Context, j *Job) (*Job, error)
	Get(ctx context.Context, tenantID, id string) (*Job, error)
	List(ctx context.Context, tenantID string, limit int) ([]*Job, error)
	Start(ctx context.Context, id string) error
	Progress(ctx context.Context, id string, pct int, message string) error
	Succeed(ctx context.Context, id string, result map[string]any) error
	Fail(ctx context.Context, id, errMessage string) error
}

// ErrNotFound is returned when a job id doesn't exist (or belongs to a
// different tenant).
var ErrNotFound = errors.New("jobs: not found")
