// Package connectorsync implements the "connector_sync" task type — a
// row-by-row copy from one Postgres connection into another. v0 streams
// in batches via SELECT on the source and INSERT on the target.
//
// Config:
//
//	{
//	  "source_connection_id": "abc-1",
//	  "source_query":         "SELECT id, email, created_at FROM raw.users",
//	  "target_connection_id": "abc-2",
//	  "target_table":         "warehouse.users",
//	  "mode":                 "append"   // "append" | "replace"
//	}
//
// Output:
//
//	{ "rows_copied": 1234, "duration_ms": 87 }
//
// Caveats: v0 uses INSERT … VALUES batches; for million-row copies a
// COPY-protocol pipe is needed (planned for step 2.5).
package connectorsync

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Satyaamm/plowered/internal/core/pipeline"
	"github.com/Satyaamm/plowered/internal/core/pipeline/tasks/taskdeps"
)

const defaultBatchSize = 500

type Executor struct {
	Conn      taskdeps.ConnFactory
	BatchSize int
}

func New(c taskdeps.ConnFactory) *Executor { return &Executor{Conn: c, BatchSize: defaultBatchSize} }

func (Executor) Type() pipeline.TaskType { return pipeline.TaskTypeConnectorSync }

func (e Executor) Execute(ctx context.Context, ec pipeline.ExecutionContext) (pipeline.Output, error) {
	cfg := ec.Task.Config
	srcID, _ := cfg["source_connection_id"].(string)
	srcQuery, _ := cfg["source_query"].(string)
	dstID, _ := cfg["target_connection_id"].(string)
	dstTable, _ := cfg["target_table"].(string)
	mode, _ := cfg["mode"].(string)
	if mode == "" {
		mode = "append"
	}
	if srcID == "" || srcQuery == "" || dstID == "" || dstTable == "" {
		return pipeline.Output{}, errors.New("connector_sync: source_connection_id, source_query, target_connection_id, target_table all required")
	}
	if e.Conn == nil {
		return pipeline.Output{}, errors.New("connector_sync: no connection factory configured")
	}
	batchSize := e.BatchSize
	if batchSize <= 0 {
		batchSize = defaultBatchSize
	}

	src, err := e.dial(ctx, ec.Run.TenantID, srcID)
	if err != nil {
		return pipeline.Output{}, fmt.Errorf("dial source: %w", err)
	}
	defer src.Close(context.Background())

	dst, err := e.dial(ctx, ec.Run.TenantID, dstID)
	if err != nil {
		return pipeline.Output{}, fmt.Errorf("dial target: %w", err)
	}
	defer dst.Close(context.Background())

	runCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	if strings.EqualFold(mode, "replace") {
		if _, err := dst.Exec(runCtx, fmt.Sprintf("TRUNCATE %s", dstTable)); err != nil {
			return pipeline.Output{}, fmt.Errorf("truncate target: %w", err)
		}
	}

	rows, err := src.Query(runCtx, srcQuery)
	if err != nil {
		return pipeline.Output{}, fmt.Errorf("source query: %w", err)
	}
	defer rows.Close()

	cols := rows.FieldDescriptions()
	colNames := make([]string, len(cols))
	for i, c := range cols {
		colNames[i] = string(c.Name)
	}

	ec.Log(ctx, "info", "connector_sync: copying %s -> %s (mode=%s, batch=%d)", "source", dstTable, mode, batchSize)
	start := time.Now()
	copied, err := streamCopy(runCtx, rows, dst, dstTable, colNames, batchSize)
	if err != nil {
		return pipeline.Output{}, fmt.Errorf("copy: %w", err)
	}
	ec.Log(ctx, "info", "connector_sync: copied %d rows in %dms", copied, time.Since(start).Milliseconds())

	edges := []pipeline.LineageEdgeProposal{{
		SourceQN: extractFirstSource(srcQuery), TargetQN: dstTable, Op: "copy",
		Properties: map[string]any{"task_id": ec.Task.ID, "mode": mode, "rows": copied},
	}}

	return pipeline.Output{
		Properties: map[string]any{
			"rows_copied": copied,
			"duration_ms": time.Since(start).Milliseconds(),
			"mode":        mode,
			"target":      dstTable,
		},
		LineageEdges: edges,
	}, nil
}

func (e Executor) dial(ctx context.Context, tenantID, connID string) (*pgx.Conn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return e.Conn(dialCtx, tenantID, connID)
}

func streamCopy(ctx context.Context, rows pgx.Rows, dst *pgx.Conn, table string, cols []string, batchSize int) (int64, error) {
	var (
		total  int64
		batch  [][]any
		insert = buildInsertPrefix(table, cols)
	)
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return total, err
		}
		batch = append(batch, vals)
		if len(batch) >= batchSize {
			if err := flushBatch(ctx, dst, insert, batch); err != nil {
				return total, err
			}
			total += int64(len(batch))
			batch = batch[:0]
		}
	}
	if err := rows.Err(); err != nil {
		return total, err
	}
	if len(batch) > 0 {
		if err := flushBatch(ctx, dst, insert, batch); err != nil {
			return total, err
		}
		total += int64(len(batch))
	}
	return total, nil
}

func buildInsertPrefix(table string, cols []string) string {
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES ", table, strings.Join(cols, ","))
}

func flushBatch(ctx context.Context, dst *pgx.Conn, prefix string, batch [][]any) error {
	if len(batch) == 0 {
		return nil
	}
	cols := len(batch[0])
	args := make([]any, 0, len(batch)*cols)
	tuples := make([]string, 0, len(batch))
	idx := 1
	for _, row := range batch {
		ph := make([]string, cols)
		for i := 0; i < cols; i++ {
			ph[i] = fmt.Sprintf("$%d", idx)
			idx++
		}
		tuples = append(tuples, "("+strings.Join(ph, ",")+")")
		args = append(args, row...)
	}
	_, err := dst.Exec(ctx, prefix+strings.Join(tuples, ","), args...)
	return err
}

func extractFirstSource(sql string) string {
	low := strings.ToLower(sql)
	idx := strings.Index(low, " from ")
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(sql[idx+6:])
	for i, r := range rest {
		if r == ' ' || r == '\t' || r == '\n' || r == ';' || r == ',' {
			return rest[:i]
		}
	}
	return rest
}
