package worker_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Satyaamm/plowered/internal/core/quality"
	"github.com/Satyaamm/plowered/internal/storage"
	"github.com/Satyaamm/plowered/internal/worker"
)

// fakeDS produces a fixed scalar; the row_count check reads it as a count.
type fakeDS struct{ rowCount int64 }

func (f *fakeDS) QueryScalar(_ context.Context, _ string, _ ...any) (any, error) {
	return f.rowCount, nil
}
func (f *fakeDS) QueryTime(_ context.Context, _ string, _ ...any) (time.Time, error) {
	return time.Now(), nil
}

func TestHandleQualityRunPersistsResult(t *testing.T) {
	store := quality.NewMemoryStore()
	ctx := storage.WithTenant(context.Background(), "t1")
	check, err := store.CreateCheck(ctx, &quality.Check{
		TenantID: "t1", AssetID: "a1", Name: "rc", Type: quality.CheckRowCount,
		Config: map[string]any{"table": "orders", "min": 1.0},
	})
	if err != nil {
		t.Fatal(err)
	}

	h := &worker.Handlers{
		Quality:   store,
		Scheduler: quality.NewScheduler(quality.NewRunner(), quality.SchedulerConfig{}),
		Resolver: func(_ context.Context, _, _ string) (quality.DataSource, error) {
			return &fakeDS{rowCount: 5}, nil
		},
	}

	payload, _ := jsonMarshal(worker.QualityRunPayload{
		TenantID: "t1", CheckID: check.ID, SourceID: "s1",
	})
	if err := h.HandleQualityRun(context.Background(), payload); err != nil {
		t.Fatalf("handle: %v", err)
	}

	runs, _ := store.ListRuns(ctx, "t1", check.ID, 10)
	if len(runs) != 1 {
		t.Fatalf("runs = %d", len(runs))
	}
	if runs[0].Outcome != quality.OutcomePass || runs[0].Value != 5 {
		t.Errorf("unexpected run: %+v", runs[0])
	}
}

func TestHandleQualityRunResolverErrorPersistsErrorOutcome(t *testing.T) {
	store := quality.NewMemoryStore()
	ctx := storage.WithTenant(context.Background(), "t1")
	check, _ := store.CreateCheck(ctx, &quality.Check{
		TenantID: "t1", AssetID: "a1", Name: "rc", Type: quality.CheckRowCount,
		Config: map[string]any{"table": "orders"},
	})

	h := &worker.Handlers{
		Quality:   store,
		Scheduler: quality.NewScheduler(quality.NewRunner(), quality.SchedulerConfig{}),
		Resolver: func(_ context.Context, _, _ string) (quality.DataSource, error) {
			return nil, errors.New("conn refused")
		},
	}
	payload, _ := jsonMarshal(worker.QualityRunPayload{
		TenantID: "t1", CheckID: check.ID, SourceID: "s1",
	})
	// Resolver errors should NOT bubble out — they're recorded as
	// OutcomeError so the operator sees the failure.
	if err := h.HandleQualityRun(context.Background(), payload); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	runs, _ := store.ListRuns(ctx, "t1", check.ID, 10)
	if len(runs) != 1 || runs[0].Outcome != quality.OutcomeError {
		t.Errorf("unexpected runs: %+v", runs)
	}
}

func TestHandleQualityRunRejectsBadPayload(t *testing.T) {
	h := &worker.Handlers{}
	if err := h.HandleQualityRun(context.Background(), []byte("not json")); err == nil {
		t.Error("expected unmarshal error")
	}
}

func TestSyncEnqueuerRunsAsync(t *testing.T) {
	store := quality.NewMemoryStore()
	ctx := storage.WithTenant(context.Background(), "t1")
	check, _ := store.CreateCheck(ctx, &quality.Check{
		TenantID: "t1", AssetID: "a1", Name: "rc", Type: quality.CheckRowCount,
		Config: map[string]any{"table": "orders"},
	})

	enq := worker.NewSyncEnqueuer(&worker.Handlers{
		Quality:   store,
		Scheduler: quality.NewScheduler(quality.NewRunner(), quality.SchedulerConfig{}),
		Resolver: func(_ context.Context, _, _ string) (quality.DataSource, error) {
			return &fakeDS{rowCount: 7}, nil
		},
	})
	if err := enq.EnqueueQualityRun(context.Background(), worker.QualityRunPayload{
		TenantID: "t1", CheckID: check.ID,
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// SyncEnqueuer fires a goroutine; poll briefly for completion.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		runs, _ := store.ListRuns(ctx, "t1", check.ID, 10)
		if len(runs) > 0 {
			if runs[0].Value != 7 {
				t.Errorf("value = %v", runs[0].Value)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("sync enqueuer did not run within deadline")
}
