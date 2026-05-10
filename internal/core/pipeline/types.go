// Package pipeline owns the orchestration domain: Pipeline, Task, Run,
// TaskRun, and the state machines that connect them. Storage is abstracted
// behind storage.PipelineStore; execution sits in pipeline.Runner.
//
// See ORCHESTRATION.md for the design rationale.
package pipeline

import (
	"time"
)

// Pipeline is a named DAG of Tasks owned by one tenant.
type Pipeline struct {
	ID            string
	TenantID      string
	Name          string
	Description   string
	Tasks         []Task
	Schedule      *Schedule // nil = manual-trigger only
	Concurrency   int       // max parallel tasks within a single depth level (default 4)
	FailFast      bool      // true: cancel siblings on any task failure
	CreatedAt     time.Time
	UpdatedAt     time.Time
	CreatedBy     string
	UpdatedBy     string
}

// Task is one node in the pipeline DAG.
type Task struct {
	ID        string         // unique within the pipeline
	Type      TaskType
	Config    map[string]any // task-type-specific
	DependsOn []string       // IDs of tasks this one depends on
	Retry     RetryPolicy
	Timeout   time.Duration  // 0 = inherit pipeline default
	Outputs   []string       // qualified names this task produces (for lineage)
}

// TaskType discriminates task behaviour. Built-in types live under
// internal/core/pipeline/tasks/<type>; custom types register through the
// TaskRegistry.
type TaskType string

const (
	TaskTypeSQL           TaskType = "sql"
	TaskTypeQualityCheck  TaskType = "quality_check"
	TaskTypeConnectorSync TaskType = "connector_sync"
	TaskTypeWebhook       TaskType = "webhook"
	TaskTypeTransformRun  TaskType = "transform_run"
)

// Schedule fires Runs on a cron expression.
type Schedule struct {
	Cron     string
	Timezone string // IANA, e.g. "UTC" or "America/Los_Angeles"
	Enabled  bool
}

// RetryPolicy controls task-level retry behaviour. Zero values disable
// retries and inherit pipeline defaults.
type RetryPolicy struct {
	MaxAttempts    int
	InitialBackoff time.Duration
	Multiplier     float64
	MaxBackoff     time.Duration
}

// DefaultRetry is applied to tasks without their own policy.
var DefaultRetry = RetryPolicy{
	MaxAttempts:    3,
	InitialBackoff: 5 * time.Second,
	Multiplier:     2.0,
	MaxBackoff:     5 * time.Minute,
}

// BackoffFor returns the wait time before attempt N (1-indexed).
func (r RetryPolicy) BackoffFor(attempt int) time.Duration {
	if attempt <= 1 {
		return 0
	}
	wait := float64(r.InitialBackoff)
	for i := 1; i < attempt-1; i++ {
		wait *= r.Multiplier
	}
	if wait > float64(r.MaxBackoff) {
		return r.MaxBackoff
	}
	return time.Duration(wait)
}

// Run is one execution instance of a Pipeline.
type Run struct {
	ID             string
	TenantID       string
	PipelineID     string
	Status         RunStatus
	StartedAt      time.Time
	FinishedAt     time.Time
	ScheduledAt    time.Time // when the schedule fired (or now() for manual triggers)
	TriggeredBy    string    // user id, "schedule", "api"
	IdempotencyKey string    // pipeline_id+scheduled_at hash for schedule dedup
	LastHeartbeat  time.Time // bumped by the runner; reaper marks runs stuck if stale
}

// RunStatus enumerates Run lifecycle states.
type RunStatus string

const (
	RunQueued    RunStatus = "queued"
	RunRunning   RunStatus = "running"
	RunSucceeded RunStatus = "succeeded"
	RunFailed    RunStatus = "failed"
	RunCancelled RunStatus = "cancelled"
)

// TaskRun is one execution of one task within a Run.
type TaskRun struct {
	ID           string
	TenantID     string
	RunID        string
	TaskID       string
	Status       TaskStatus
	AttemptCount int
	StartedAt    time.Time
	FinishedAt   time.Time
	Error        string
	Output       map[string]any
	DeadLetter   bool
}

// TaskStatus enumerates TaskRun lifecycle states.
type TaskStatus string

const (
	TaskQueued    TaskStatus = "queued"
	TaskRunning   TaskStatus = "running"
	TaskSucceeded TaskStatus = "succeeded"
	TaskFailed    TaskStatus = "failed"
	TaskSkipped   TaskStatus = "skipped"
	TaskRetrying  TaskStatus = "retrying"
)

// IsTerminal reports whether the status is end-of-life for a TaskRun.
func (s TaskStatus) IsTerminal() bool {
	switch s {
	case TaskSucceeded, TaskFailed, TaskSkipped:
		return true
	}
	return false
}
