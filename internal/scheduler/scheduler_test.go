package scheduler_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Satyaamm/plowered/internal/core/events"
	"github.com/Satyaamm/plowered/internal/core/pipeline"
	"github.com/Satyaamm/plowered/internal/scheduler"
	"github.com/Satyaamm/plowered/internal/worker"
)

// recorder counts EnqueuePipelineRun calls and stores the run IDs.
type recorder struct {
	count atomic.Int32
}

func (r *recorder) EnqueuePipelineRun(_ context.Context, _ worker.PipelineRunPayload) error {
	r.count.Add(1)
	return nil
}
func (r *recorder) EnqueueQualityRun(_ context.Context, _ worker.QualityRunPayload) error {
	return nil
}

// The scheduler only triggers pipelines + quality; stub the rest of the
// Enqueuer surface so this test double still satisfies the interface
// after it grew (crawl, classify, search:reindex).
func (*recorder) EnqueueCrawlConnection(context.Context, worker.CrawlConnectionPayload) error {
	return nil
}
func (*recorder) EnqueueClassifyConnection(context.Context, worker.ClassifyConnectionPayload) error {
	return nil
}
func (*recorder) EnqueueSearchReindex(context.Context, worker.SearchReindexPayload) error {
	return nil
}

// fixedClock returns a configurable time. Tests advance it manually.
type fixedClock struct {
	mu sync.Mutex // tests don't race; just for paranoia
	t  time.Time
}

func newClock(t time.Time) *fixedClock { return &fixedClock{t: t} }
func (c *fixedClock) now() time.Time   { return c.t }

func TestSchedulerFiresDuePipeline(t *testing.T) {
	store := pipeline.NewMemoryStore()
	enq := &recorder{}

	// Pipeline whose cron fired one second ago.
	_, _ = store.CreatePipeline(context.Background(), &pipeline.Pipeline{
		TenantID: "t1", Name: "every minute",
		Schedule: &pipeline.Schedule{Cron: "* * * * *", Enabled: true},
	})

	clock := newClock(time.Date(2026, 5, 8, 12, 0, 30, 0, time.UTC))
	s := scheduler.New(store, enq)
	s.Now = clock.now

	if err := scheduler.FireDueForTest(s, context.Background()); err != nil {
		t.Fatalf("fireDue: %v", err)
	}
	if enq.count.Load() != 1 {
		t.Errorf("enqueue count = %d, want 1", enq.count.Load())
	}

	// Re-running the same tick must not re-fire (same scheduled_at).
	if err := scheduler.FireDueForTest(s, context.Background()); err != nil {
		t.Fatalf("fireDue 2: %v", err)
	}
	if enq.count.Load() != 1 {
		t.Errorf("dedup failed: count = %d", enq.count.Load())
	}

	// Advance time past the next minute boundary; should fire again.
	clock.t = clock.t.Add(2 * time.Minute)
	if err := scheduler.FireDueForTest(s, context.Background()); err != nil {
		t.Fatalf("fireDue 3: %v", err)
	}
	if enq.count.Load() != 2 {
		t.Errorf("second tick missed: count = %d", enq.count.Load())
	}
}

func TestSchedulerSkipsDisabledPipelines(t *testing.T) {
	store := pipeline.NewMemoryStore()
	enq := &recorder{}

	_, _ = store.CreatePipeline(context.Background(), &pipeline.Pipeline{
		TenantID: "t1", Name: "off",
		Schedule: &pipeline.Schedule{Cron: "* * * * *", Enabled: false},
	})
	_, _ = store.CreatePipeline(context.Background(), &pipeline.Pipeline{
		TenantID: "t1", Name: "no-schedule",
	})

	s := scheduler.New(store, enq)
	s.Now = func() time.Time { return time.Now() }
	if err := scheduler.FireDueForTest(s, context.Background()); err != nil {
		t.Fatalf("fireDue: %v", err)
	}
	if enq.count.Load() != 0 {
		t.Errorf("expected 0 fires, got %d", enq.count.Load())
	}
}

func TestReaperMarksStuckRuns(t *testing.T) {
	store := pipeline.NewMemoryStore()
	bus := events.NewMemoryBus()
	var failedCount atomic.Int32
	bus.Subscribe(events.FuncSubscriber(func(_ context.Context, e events.Event) {
		if e.Type == events.RunFailed {
			failedCount.Add(1)
		}
	}))

	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	_, _ = store.CreatePipeline(context.Background(), &pipeline.Pipeline{
		TenantID: "t1", Name: "p1",
	})

	// Run that started 10 minutes ago and is still "running".
	_, _ = store.CreateRun(context.Background(), &pipeline.Run{
		ID: "stuck-1", TenantID: "t1", PipelineID: "p1",
		Status: pipeline.RunRunning, StartedAt: now.Add(-10 * time.Minute),
	})
	// Run that started 30 seconds ago — too recent to be stuck.
	_, _ = store.CreateRun(context.Background(), &pipeline.Run{
		ID: "fresh-1", TenantID: "t1", PipelineID: "p1",
		Status: pipeline.RunRunning, StartedAt: now.Add(-30 * time.Second),
	})

	s := scheduler.New(store, &recorder{})
	s.Events = bus
	s.Now = func() time.Time { return now }
	s.Config = scheduler.Config{StuckAfter: 5 * time.Minute}

	if err := scheduler.ReapForTest(s, context.Background()); err != nil {
		t.Fatalf("reap: %v", err)
	}
	stuck, _ := store.GetRun(context.Background(), "stuck-1")
	if stuck.Status != pipeline.RunFailed {
		t.Errorf("stuck run status = %s, want failed", stuck.Status)
	}
	fresh, _ := store.GetRun(context.Background(), "fresh-1")
	if fresh.Status != pipeline.RunRunning {
		t.Errorf("fresh run status = %s, want running", fresh.Status)
	}
	if failedCount.Load() != 1 {
		t.Errorf("expected 1 RunFailed event, got %d", failedCount.Load())
	}
}

func TestSchedulerRunBlocksUntilContextCancelled(t *testing.T) {
	s := scheduler.New(pipeline.NewMemoryStore(), &recorder{})
	s.Config = scheduler.Config{
		CronInterval:   10 * time.Millisecond,
		ReaperInterval: 10 * time.Millisecond,
		StuckAfter:     time.Second,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()
	time.Sleep(40 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler did not exit after ctx cancel")
	}
}
