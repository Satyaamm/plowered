// Package sql implements the "sql" task type — a single SQL statement
// (or batch of semicolon-separated statements) executed against a
// customer-defined Postgres connection.
//
// Config:
//
//	{
//	  "connection_id": "abc-123",
//	  "statement":     "DELETE FROM staging.events WHERE created_at < ...",
//	  "timeout_seconds": 60
//	}
//
// Output:
//
//	{ "rows_affected": 1234, "duration_ms": 87 }
package sql

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Satyaamm/plowered/internal/core/pipeline"
	"github.com/Satyaamm/plowered/internal/core/pipeline/tasks/taskdeps"
)

// Executor runs the "sql" task type.
type Executor struct {
	Conn taskdeps.ConnFactory
}

func New(c taskdeps.ConnFactory) *Executor { return &Executor{Conn: c} }

func (Executor) Type() pipeline.TaskType { return pipeline.TaskTypeSQL }

func (e Executor) Execute(ctx context.Context, ec pipeline.ExecutionContext) (pipeline.Output, error) {
	cfg := ec.Task.Config
	connID, _ := cfg["connection_id"].(string)
	stmt, _ := cfg["statement"].(string)
	if connID == "" || stmt == "" {
		return pipeline.Output{}, errors.New("sql: connection_id and statement are required")
	}
	timeout := readTimeout(cfg, 60*time.Second)
	if e.Conn == nil {
		return pipeline.Output{}, errors.New("sql: no connection factory configured")
	}

	ec.Log(ctx, "info", "sql: dialing connection %s", connID)
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	conn, err := e.Conn(dialCtx, ec.Run.TenantID, connID)
	cancel()
	if err != nil {
		return pipeline.Output{}, fmt.Errorf("dial: %w", err)
	}
	defer conn.Close(context.Background())

	execCtx, execCancel := context.WithTimeout(ctx, timeout)
	defer execCancel()

	preview := stmt
	if len(preview) > 200 {
		preview = preview[:200] + "…"
	}
	ec.Log(ctx, "info", "sql: executing %s", preview)
	start := time.Now()
	tag, err := conn.Exec(execCtx, stmt)
	if err != nil {
		return pipeline.Output{}, fmt.Errorf("exec: %w", err)
	}
	ec.Log(ctx, "info", "sql: %s in %dms (rows=%d)", tag.String(), time.Since(start).Milliseconds(), tag.RowsAffected())
	return pipeline.Output{
		Properties: map[string]any{
			"rows_affected": tag.RowsAffected(),
			"command":       tag.String(),
			"duration_ms":   time.Since(start).Milliseconds(),
		},
	}, nil
}

func readTimeout(cfg map[string]any, def time.Duration) time.Duration {
	switch v := cfg["timeout_seconds"].(type) {
	case float64:
		if v > 0 {
			return time.Duration(v) * time.Second
		}
	case int:
		if v > 0 {
			return time.Duration(v) * time.Second
		}
	}
	return def
}
