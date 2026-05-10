package scheduler_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/pipeline"
	"github.com/Satyaamm/plowered/internal/scheduler"
	"github.com/Satyaamm/plowered/internal/storage"
	"github.com/Satyaamm/plowered/internal/storage/postgres"
)

// TestSchedulerAgainstLivePostgres exercises the cron + dedup path against a
// real Postgres pool. Skipped when PLOWERED_TEST_DATABASE_URL is unset, so
// the unit suite stays hermetic.
func TestSchedulerAgainstLivePostgres(t *testing.T) {
	dsn := os.Getenv("PLOWERED_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("PLOWERED_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	store := postgres.NewPipelineStore(pool)
	tenantCtx := storage.WithTenant(ctx, "scheduler-test")

	// Clean any prior rows so test is deterministic.
	_, _ = pool.Exec(ctx, `DELETE FROM pipeline_runs WHERE tenant_id = 'scheduler-test'`)
	_, _ = pool.Exec(ctx, `DELETE FROM pipelines     WHERE tenant_id = 'scheduler-test'`)

	p, err := store.CreatePipeline(tenantCtx, &pipeline.Pipeline{
		Name:     "live-cron-" + time.Now().Format("150405.000"),
		Schedule: &pipeline.Schedule{Cron: "* * * * *", Enabled: true},
	})
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}

	enq := &recorder{}
	s := scheduler.New(store, enq)
	clock := newClock(time.Now().UTC().Add(-30 * time.Second))
	s.Now = clock.now

	if err := scheduler.FireDueForTest(s, ctx); err != nil {
		t.Fatalf("fire: %v", err)
	}

	// Even with no due-time the in-process dedup+postgres write should be
	// consistent — verify either zero or one fire, not more.
	if c := enq.count.Load(); c > 1 {
		t.Errorf("fire count = %d, want <= 1", c)
	}

	// Restore real time and fire once more — at most one new run.
	s.Now = time.Now
	if err := scheduler.FireDueForTest(s, ctx); err != nil {
		t.Fatalf("fire 2: %v", err)
	}

	runs, err := store.ListRuns(tenantCtx, "scheduler-test", p.ID, 10)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) > 2 {
		t.Errorf("runs created = %d, want <= 2", len(runs))
	}
}
