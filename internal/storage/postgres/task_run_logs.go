package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/pipeline"
)

// LogStore is the Postgres-backed pipeline.LogStore. Append is fire-and-
// forget from the runner's perspective (the runner ignores errors so a
// down log table doesn't break execution); List drives the SSE tail.
type LogStore struct {
	pool *pgxpool.Pool
}

func NewLogStore(p *pgxpool.Pool) *LogStore { return &LogStore{pool: p} }

// Append inserts one log line. Errors are returned for callers that care
// (tests, an oncall debugger), but the runner currently swallows them.
func (s *LogStore) Append(ctx context.Context, line pipeline.LogLine) error {
	if line.RunID == "" || line.TenantID == "" {
		return fmt.Errorf("log: tenant_id and run_id required")
	}
	level := line.Level
	if level == "" {
		level = "info"
	}
	const q = `
		INSERT INTO task_run_logs (tenant_id, run_id, task_run_id, task_id, level, line)
		VALUES ($1, $2, NULLIF($3, '')::uuid, $4, $5, $6)`
	_, err := s.pool.Exec(ctx, q, line.TenantID, line.RunID, line.TaskRunID, line.TaskID, level, line.Line)
	if err != nil {
		return fmt.Errorf("insert task_run_log: %w", err)
	}
	return nil
}

// List returns up to `limit` lines for runID with id > sinceID, ordered
// by id ascending so the SSE consumer can use the last id of the batch
// as the next cursor.
func (s *LogStore) List(ctx context.Context, runID string, sinceID int64, limit int) ([]pipeline.LogLine, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	const q = `
		SELECT id, tenant_id, run_id, COALESCE(task_run_id::text, ''), task_id, level, line, created_at
		  FROM task_run_logs
		 WHERE run_id = $1::uuid
		   AND id > $2
		 ORDER BY id ASC
		 LIMIT $3`
	rows, err := s.pool.Query(ctx, q, runID, sinceID, limit)
	if err != nil {
		return nil, fmt.Errorf("query task_run_logs: %w", err)
	}
	defer rows.Close()
	out := make([]pipeline.LogLine, 0, limit)
	for rows.Next() {
		var ll pipeline.LogLine
		if err := rows.Scan(&ll.ID, &ll.TenantID, &ll.RunID, &ll.TaskRunID, &ll.TaskID, &ll.Level, &ll.Line, &ll.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, ll)
	}
	return out, rows.Err()
}

// Compile-time interface check.
var _ pipeline.LogStore = (*LogStore)(nil)
