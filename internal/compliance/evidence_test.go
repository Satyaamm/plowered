package compliance_test

import (
	"context"
	"testing"
	"time"

	"github.com/Satyaamm/plowered/internal/compliance"
	"github.com/Satyaamm/plowered/internal/core/audit"
	"github.com/Satyaamm/plowered/internal/core/pipeline"
)

func TestCollectSummarizesRunsAndAudit(t *testing.T) {
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	pStore := pipeline.NewMemoryStore()

	mkRun := func(status pipeline.RunStatus, scheduledAt time.Time) {
		_, _ = pStore.CreateRun(context.Background(), &pipeline.Run{
			TenantID: "t1", PipelineID: "p1",
			Status: status, ScheduledAt: scheduledAt,
		})
	}
	mkRun(pipeline.RunSucceeded, now.Add(-1*time.Hour))
	mkRun(pipeline.RunSucceeded, now.Add(-2*time.Hour))
	mkRun(pipeline.RunFailed, now.Add(-30*time.Minute))
	mkRun(pipeline.RunRunning, now.Add(-2*time.Minute))   // not stuck
	mkRun(pipeline.RunRunning, now.Add(-10*time.Minute))  // stuck
	// Use the writer's Started filter via Status=running + heartbeat fields.
	// ListStuckRuns checks LastHeartbeat || StartedAt. Set StartedAt on
	// the second running entry so it qualifies as stuck.
	if runs, _ := pStore.ListRuns(context.Background(), "t1", "p1", 100); len(runs) > 0 {
		for _, r := range runs {
			if r.Status == pipeline.RunRunning && now.Sub(r.ScheduledAt) > 5*time.Minute {
				r.StartedAt = r.ScheduledAt
				_ = pStore.UpdateRun(context.Background(), r)
			}
		}
	}

	auditW := audit.NewMemoryWriter()
	_ = auditW.Emit(context.Background(), audit.Event{
		TenantID: "t1", Action: "pipeline.create", ActorID: "u1", ActorKind: "user",
		ResourceType: "pipeline", ResourceID: "p1", CreatedAt: now.Add(-1 * time.Hour),
	})
	_ = auditW.Emit(context.Background(), audit.Event{
		TenantID: "t1", Action: "pipeline.trigger", ActorID: "u1", ActorKind: "user",
		ResourceType: "pipeline", ResourceID: "p1", CreatedAt: now.Add(-30 * time.Minute),
	})

	c := &compliance.Collector{
		Pipelines: pStore,
		Audit:     auditW,
		Now:       func() time.Time { return now },
	}
	rep, err := c.Collect(context.Background(), "t1")
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if rep.PipelineRuns.Total != 5 {
		t.Errorf("total runs = %d, want 5", rep.PipelineRuns.Total)
	}
	if rep.PipelineRuns.Succeeded != 2 || rep.PipelineRuns.Failed != 1 {
		t.Errorf("succeeded/failed counts wrong: %+v", rep.PipelineRuns)
	}
	// FailureRate = failed / (total - running) = 1 / (5-2) = 0.333…
	if rep.PipelineRuns.FailureRate < 0.30 || rep.PipelineRuns.FailureRate > 0.40 {
		t.Errorf("failure rate = %v, want ~0.333", rep.PipelineRuns.FailureRate)
	}
	if rep.AuditEventsTotal != 2 {
		t.Errorf("audit events = %d, want 2", rep.AuditEventsTotal)
	}
	if rep.AuditEventsByAction["pipeline.create"] != 1 {
		t.Errorf("missing pipeline.create count")
	}
}

func TestCollectFlagsEmptyAudit(t *testing.T) {
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	pStore := pipeline.NewMemoryStore()
	_, _ = pStore.CreateRun(context.Background(), &pipeline.Run{
		TenantID: "t1", PipelineID: "p1",
		Status: pipeline.RunSucceeded, ScheduledAt: now.Add(-1 * time.Hour),
	})

	c := &compliance.Collector{
		Pipelines: pStore,
		Audit:     audit.NewMemoryWriter(), // empty
		Now:       func() time.Time { return now },
	}
	rep, err := c.Collect(context.Background(), "t1")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, issue := range rep.Issues {
		if contains(issue, "audit log empty") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected audit-empty issue, got %v", rep.Issues)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		(len(haystack) > len(needle) && (indexOf(haystack, needle) >= 0)))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
