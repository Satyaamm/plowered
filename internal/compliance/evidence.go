// Package compliance produces evidence artifacts for SOC 2 / GDPR / HIPAA
// audits. A daily cron calls Collector.Collect and ships the resulting
// Report to object-locked storage; auditors read it without needing
// production access.
//
// The collector is deliberately read-only. It pulls existing data
// (audit_events, pipeline_runs, quality_check_runs, policy_rules, …) and
// summarizes it; it never mutates state. See COMPLIANCE.md for the
// control matrix this evidence maps to.
package compliance

import (
	"context"
	"fmt"
	"time"

	"github.com/Satyaamm/plowered/internal/core/audit"
	"github.com/Satyaamm/plowered/internal/core/pipeline"
)

// Report is the structured artifact produced by Collect. Field names map
// to the CONTROL_MATRIX columns in COMPLIANCE.md so an auditor can grep
// for the control id and find the data.
type Report struct {
	GeneratedAt time.Time `json:"generated_at"`
	Window      Window    `json:"window"`

	// SOC 2 CC4 — system monitoring.
	PipelineRuns      RunStats   `json:"pipeline_runs"`
	StuckRunsAtCutoff int        `json:"stuck_runs_at_cutoff"`

	// SOC 2 CC6 — logical access. GDPR Art. 30 — records of processing.
	AuditEventsTotal     int            `json:"audit_events_total"`
	AuditEventsByAction  map[string]int `json:"audit_events_by_action"`

	// SOC 2 PI — processing integrity. Repeatable signal that the
	// orchestrator is running checks at the configured cadence.
	OldestPendingRunAge time.Duration `json:"oldest_pending_run_age_seconds"`

	// Issues collected during the run; non-empty means an auditor should
	// look more carefully. The collector itself never returns an error
	// for these — the absence of a clean run IS the audit signal.
	Issues []string `json:"issues,omitempty"`
}

// Window is the inclusive [Since, Until] interval the report covers.
type Window struct {
	Since time.Time `json:"since"`
	Until time.Time `json:"until"`
}

// RunStats summarizes pipeline runs in the window.
type RunStats struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Cancelled int `json:"cancelled"`
	Running   int `json:"running"`

	// FailureRate is succeeded == 0 → 1.0; otherwise failed / (total -
	// running). Reported as a fraction in [0, 1].
	FailureRate float64 `json:"failure_rate"`
}

// PipelineSource is the read surface the collector needs from the pipeline
// store. Both MemoryStore and PostgresStore satisfy it via ListRuns +
// ListStuckRuns (added in Batch 4).
type PipelineSource interface {
	ListRuns(ctx context.Context, tenantID, pipelineID string, limit int) ([]*pipeline.Run, error)
	ListStuckRuns(ctx context.Context, olderThan time.Time) ([]*pipeline.Run, error)
}

// Collector wires the read sources. Construct one at process startup
// (alongside the API server) and call Collect from a cron job.
type Collector struct {
	Pipelines PipelineSource
	Audit     audit.Reader
	Now       func() time.Time

	// LookbackWindow is how far into the past the report covers. Defaults
	// to 24 hours when zero.
	LookbackWindow time.Duration

	// StuckCutoff matches the scheduler's StuckAfter setting. Defaults to
	// 5 minutes when zero.
	StuckCutoff time.Duration
}

// Collect builds a Report for the configured tenant.
func (c *Collector) Collect(ctx context.Context, tenantID string) (*Report, error) {
	if c.Now == nil {
		c.Now = time.Now
	}
	now := c.Now().UTC()
	lookback := c.LookbackWindow
	if lookback <= 0 {
		lookback = 24 * time.Hour
	}
	stuckCutoff := c.StuckCutoff
	if stuckCutoff <= 0 {
		stuckCutoff = 5 * time.Minute
	}

	report := &Report{
		GeneratedAt: now,
		Window:      Window{Since: now.Add(-lookback), Until: now},
	}

	// Pipeline runs.
	if c.Pipelines != nil {
		runs, err := c.Pipelines.ListRuns(ctx, tenantID, "", 1000)
		if err != nil {
			return nil, fmt.Errorf("compliance: list runs: %w", err)
		}
		var oldestPendingAge time.Duration
		for _, r := range runs {
			if r.ScheduledAt.Before(report.Window.Since) {
				continue
			}
			report.PipelineRuns.Total++
			switch r.Status {
			case pipeline.RunSucceeded:
				report.PipelineRuns.Succeeded++
			case pipeline.RunFailed:
				report.PipelineRuns.Failed++
			case pipeline.RunCancelled:
				report.PipelineRuns.Cancelled++
			case pipeline.RunRunning, pipeline.RunQueued:
				report.PipelineRuns.Running++
				if age := now.Sub(r.ScheduledAt); age > oldestPendingAge {
					oldestPendingAge = age
				}
			}
		}
		report.OldestPendingRunAge = oldestPendingAge

		denom := report.PipelineRuns.Total - report.PipelineRuns.Running
		if denom > 0 {
			report.PipelineRuns.FailureRate = float64(report.PipelineRuns.Failed) / float64(denom)
		}

		stuck, err := c.Pipelines.ListStuckRuns(ctx, now.Add(-stuckCutoff))
		if err != nil {
			return nil, fmt.Errorf("compliance: list stuck runs: %w", err)
		}
		report.StuckRunsAtCutoff = len(stuck)
		if report.StuckRunsAtCutoff > 0 {
			report.Issues = append(report.Issues,
				fmt.Sprintf("%d stuck runs older than %s — investigate scheduler reaper",
					report.StuckRunsAtCutoff, stuckCutoff))
		}
		if report.PipelineRuns.FailureRate > 0.25 {
			report.Issues = append(report.Issues,
				fmt.Sprintf("pipeline failure rate %.1f%% in last %s exceeds 25%% threshold",
					report.PipelineRuns.FailureRate*100, lookback))
		}
	}

	// Audit log.
	if c.Audit != nil {
		events, err := c.Audit.List(ctx, tenantID, 500)
		if err != nil {
			return nil, fmt.Errorf("compliance: list audit: %w", err)
		}
		report.AuditEventsByAction = make(map[string]int)
		for _, e := range events {
			if e.CreatedAt.Before(report.Window.Since) {
				continue
			}
			report.AuditEventsTotal++
			report.AuditEventsByAction[e.Action]++
		}
		// SOC 2 CC4 expects continuous monitoring — an empty audit log
		// for a tenant with active mutations is suspicious.
		if report.AuditEventsTotal == 0 && report.PipelineRuns.Total > 0 {
			report.Issues = append(report.Issues,
				"audit log empty despite pipeline activity — investigate audit writer wiring")
		}
	}

	return report, nil
}
