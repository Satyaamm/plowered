// Package transformrun implements the "transform_run" task type — a
// dbt-style materialization. It runs a SELECT against a Postgres source
// and materializes the result into a target table (CREATE TABLE AS or
// INSERT into existing). Source tables are extracted from the SELECT
// (via a small regex) and emitted as lineage edges.
//
// Config:
//
//	{
//	  "connection_id":   "abc-123",
//	  "target":          "warehouse.dim_users",   // qualified name
//	  "sql":             "SELECT id, email, created_at FROM raw.users",
//	  "materialization": "table"   // "table" | "view"  (default: table)
//	}
//
// Output:
//
//	{
//	  "rows_affected": 1234,
//	  "duration_ms":   42,
//	  "lineage": [{"from":"raw.users","to":"warehouse.dim_users"}]
//	}
package transformrun

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Satyaamm/plowered/internal/core/lineage"
	"github.com/Satyaamm/plowered/internal/core/pipeline"
	"github.com/Satyaamm/plowered/internal/core/pipeline/tasks/taskdeps"
)

type Executor struct {
	Conn       taskdeps.ConnFactory
	ColumnSink lineage.ColumnSink // optional; nil disables column-lineage capture
}

func New(c taskdeps.ConnFactory, sink lineage.ColumnSink) *Executor {
	return &Executor{Conn: c, ColumnSink: sink}
}

func (Executor) Type() pipeline.TaskType { return pipeline.TaskTypeTransformRun }

func (e Executor) Execute(ctx context.Context, ec pipeline.ExecutionContext) (pipeline.Output, error) {
	cfg := ec.Task.Config
	connID, _ := cfg["connection_id"].(string)
	target, _ := cfg["target"].(string)
	body, _ := cfg["sql"].(string)
	if connID == "" || target == "" || body == "" {
		return pipeline.Output{}, errors.New("transform_run: connection_id, target, and sql are required")
	}
	mat, _ := cfg["materialization"].(string)
	if mat == "" {
		mat = "table"
	}

	if e.Conn == nil {
		return pipeline.Output{}, errors.New("transform_run: no connection factory configured")
	}
	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	conn, err := e.Conn(dialCtx, ec.Run.TenantID, connID)
	cancel()
	if err != nil {
		return pipeline.Output{}, fmt.Errorf("dial: %w", err)
	}
	defer conn.Close(context.Background())

	execCtx, execCancel := context.WithTimeout(ctx, 10*time.Minute)
	defer execCancel()

	var stmt string
	switch strings.ToLower(mat) {
	case "view":
		stmt = fmt.Sprintf("CREATE OR REPLACE VIEW %s AS %s", target, body)
	case "table":
		stmt = fmt.Sprintf("DROP TABLE IF EXISTS %s; CREATE TABLE %s AS %s", target, target, body)
	default:
		return pipeline.Output{}, fmt.Errorf("transform_run: unknown materialization %q", mat)
	}

	ec.Log(ctx, "info", "transform_run: materializing %s as %s", target, mat)
	start := time.Now()
	tag, err := conn.Exec(execCtx, stmt)
	if err != nil {
		return pipeline.Output{}, fmt.Errorf("materialize: %w", err)
	}
	ec.Log(ctx, "info", "transform_run: %s in %dms (rows=%d)", tag.String(), time.Since(start).Milliseconds(), tag.RowsAffected())

	sources := extractSources(body)
	if len(sources) > 0 {
		ec.Log(ctx, "info", "transform_run: lineage sources %v -> %s", sources, target)
	}

	// Column-level lineage (best-effort). Failures here don't fail the
	// task — the materialization already succeeded; lineage is enrichment.
	if e.ColumnSink != nil {
		colEdges := lineage.ExtractColumns(body, target)
		if len(colEdges) > 0 {
			taskRunID := ""
			if ec.TaskRun != nil {
				taskRunID = ec.TaskRun.ID
			}
			written, misses, err := e.ColumnSink.Upsert(ctx, ec.Run.TenantID, taskRunID, colEdges)
			if err != nil {
				ec.Log(ctx, "warn", "transform_run: column lineage upsert failed: %s", err.Error())
			} else {
				ec.Log(ctx, "info", "transform_run: column lineage written=%d misses=%d", written, misses)
			}
		}
	}
	edges := make([]pipeline.LineageEdgeProposal, 0, len(sources))
	view := make([]map[string]string, 0, len(sources))
	for _, s := range sources {
		edges = append(edges, pipeline.LineageEdgeProposal{
			SourceQN: s, TargetQN: target, Op: "transform",
			Properties: map[string]any{"task_id": ec.Task.ID, "materialization": mat},
		})
		view = append(view, map[string]string{"from": s, "to": target})
	}

	return pipeline.Output{
		Properties: map[string]any{
			"rows_affected":   tag.RowsAffected(),
			"command":         tag.String(),
			"duration_ms":     time.Since(start).Milliseconds(),
			"materialization": mat,
			"target":          target,
			"lineage":         view,
		},
		LineageEdges: edges,
	}, nil
}

// fromJoinRE finds tables referenced after FROM/JOIN. Intentionally
// permissive — captures schema.table or just table — and case-insensitive.
// Subqueries and CTEs aren't perfectly handled; the v0 contract is
// "best-effort lineage, not authoritative." A real SQL parser lands in
// step 2.5.
var fromJoinRE = regexp.MustCompile(`(?i)(?:\bFROM\b|\bJOIN\b)\s+([a-zA-Z_][a-zA-Z0-9_]*(?:\.[a-zA-Z_][a-zA-Z0-9_]*)?)`)

func extractSources(sql string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, m := range fromJoinRE.FindAllStringSubmatch(sql, -1) {
		name := m[1]
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}
