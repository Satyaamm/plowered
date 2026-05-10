package pipeline

import (
	"context"
	"time"
)

// LogLine is one structured log entry produced by an executor (or by the
// runner itself for lifecycle markers like "task started"). Lines are
// append-only and queried via LogReader.
type LogLine struct {
	ID        int64     // monotonically increasing per-row id; SSE consumers tail by this
	TenantID  string
	RunID     string
	TaskRunID string // optional: empty for run-level lines
	TaskID    string // optional convenience copy of Task.ID
	Level     string // "info" | "warn" | "error"
	Line      string
	CreatedAt time.Time
}

// LogSink is what executors call to record progress. v0 uses a Postgres-
// backed implementation in internal/storage/postgres; tests can wire in
// the in-memory sink in this package.
type LogSink interface {
	Append(ctx context.Context, line LogLine) error
}

// LogReader reads back lines for a given run. SSE handlers call List with
// SinceID = the highest id they've already shipped.
type LogReader interface {
	List(ctx context.Context, runID string, sinceID int64, limit int) ([]LogLine, error)
}

// LogStore is both halves of the contract; storage adapters typically
// satisfy both.
type LogStore interface {
	LogSink
	LogReader
}

// NoopSink discards every line. Default when the runner has no sink wired
// (older binaries, tests). Keeps executor code from having to nil-check.
type NoopSink struct{}

func (NoopSink) Append(context.Context, LogLine) error { return nil }
