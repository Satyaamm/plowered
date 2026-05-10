// Package qualitycheck implements the "quality_check" task type — fires
// an existing data-quality check inline and fails the task when the check
// outcome isn't "pass". Useful as a gate inside an ETL DAG: a transform
// step finishes, the quality check runs, downstream copies/exports only
// proceed when data is healthy.
//
// Config:
//
//	{
//	  "check_id":  "abc-123",
//	  "source_id": "warehouse" // logical source key for DataSource resolver
//	}
//
// Output:
//
//	{ "outcome": "pass", "value": 0, "threshold": 0, "duration_ms": 12 }
package qualitycheck

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Satyaamm/plowered/internal/core/pipeline"
	"github.com/Satyaamm/plowered/internal/core/quality"
)

// Resolver returns a quality.DataSource for the given (tenant, source) pair.
// Identical shape to the worker's DataSourceResolver; duplicated here to
// avoid an import cycle (worker → pipeline → worker).
type Resolver func(ctx context.Context, tenantID, sourceID string) (quality.DataSource, error)

type Executor struct {
	Store     quality.Store
	Scheduler *quality.Scheduler
	Resolver  Resolver
}

func New(s quality.Store, sched *quality.Scheduler, r Resolver) *Executor {
	return &Executor{Store: s, Scheduler: sched, Resolver: r}
}

func (Executor) Type() pipeline.TaskType { return pipeline.TaskTypeQualityCheck }

func (e Executor) Execute(ctx context.Context, ec pipeline.ExecutionContext) (pipeline.Output, error) {
	if e.Store == nil || e.Scheduler == nil || e.Resolver == nil {
		return pipeline.Output{}, errors.New("quality_check: deps not configured")
	}
	cfg := ec.Task.Config
	checkID, _ := cfg["check_id"].(string)
	if checkID == "" {
		return pipeline.Output{}, errors.New("quality_check: check_id is required")
	}
	sourceID, _ := cfg["source_id"].(string)

	check, err := e.Store.GetCheck(ctx, checkID)
	if err != nil {
		return pipeline.Output{}, fmt.Errorf("load check: %w", err)
	}
	ds, err := e.Resolver(ctx, ec.Run.TenantID, sourceID)
	if err != nil {
		return pipeline.Output{}, fmt.Errorf("resolve datasource: %w", err)
	}

	ec.Log(ctx, "info", "quality_check: running %s (type=%s severity=%s) on %s", check.ID, check.Type, check.Severity, sourceID)
	start := time.Now()
	cr := e.Scheduler.Schedule(ctx, *check, ds, quality.RunOptions{Source: ec.Run.TenantID + ":" + sourceID})
	if _, err := e.Store.RecordRun(ctx, cr); err != nil {
		return pipeline.Output{}, fmt.Errorf("record run: %w", err)
	}
	ec.Log(ctx, "info", "quality_check: outcome=%s value=%v threshold=%v", cr.Outcome, cr.Value, cr.Threshold)

	props := map[string]any{
		"check_id":    check.ID,
		"outcome":     string(cr.Outcome),
		"value":       cr.Value,
		"threshold":   cr.Threshold,
		"duration_ms": time.Since(start).Milliseconds(),
	}
	if cr.Outcome != quality.OutcomePass {
		return pipeline.Output{Properties: props}, fmt.Errorf("quality check %q failed: %s (value=%v threshold=%v)", check.ID, cr.Diagnostic, cr.Value, cr.Threshold)
	}
	return pipeline.Output{Properties: props}, nil
}
