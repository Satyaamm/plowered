// Package scheduler runs the two background loops that close the
// orchestration loop: the cron scanner that fires schedule-driven Runs and
// the reaper that marks stuck Runs failed.
//
// Both loops are single-leader-friendly: they tolerate multiple concurrent
// instances because the cron scanner keys writes on
// (pipeline_id, scheduled_at) idempotency, and the reaper is an UPDATE that
// no-ops when status has already moved on.
package scheduler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync"
	"time"

	cronlib "github.com/robfig/cron/v3"

	"github.com/Satyaamm/plowered/internal/core/events"
	"github.com/Satyaamm/plowered/internal/core/pipeline"
	"github.com/Satyaamm/plowered/internal/worker"
)

// Config controls the cadence of both loops. Zero values fall back to safe
// defaults documented inline.
type Config struct {
	// CronInterval is how often to scan for schedule-driven pipelines.
	// Default: 30 seconds (catches minute-level cron expressions reliably).
	CronInterval time.Duration

	// ReaperInterval is how often to scan for stuck runs.
	// Default: 1 minute.
	ReaperInterval time.Duration

	// StuckAfter declares "running" runs stuck if their heartbeat (or
	// started_at) is older than this window. Default: 5 minutes.
	StuckAfter time.Duration
}

func (c Config) withDefaults() Config {
	if c.CronInterval <= 0 {
		c.CronInterval = 30 * time.Second
	}
	if c.ReaperInterval <= 0 {
		c.ReaperInterval = 1 * time.Minute
	}
	if c.StuckAfter <= 0 {
		c.StuckAfter = 5 * time.Minute
	}
	return c
}

// Scheduler bundles the cron and reaper loops behind a single Run lifecycle.
type Scheduler struct {
	Repo     pipeline.Repo
	Enqueuer worker.Enqueuer
	Events   events.Bus // optional; reaper publishes RunFailed when set
	Logger   *slog.Logger
	Now      func() time.Time
	Config   Config

	parser cronlib.Parser

	// lastFiredAt tracks the most recent ScheduledAt we issued per pipeline,
	// so we don't repeatedly re-fire the same cron tick within a process.
	mu          sync.Mutex
	lastFiredAt map[string]time.Time
}

// New builds a Scheduler with sensible defaults.
func New(repo pipeline.Repo, enq worker.Enqueuer) *Scheduler {
	return &Scheduler{
		Repo:        repo,
		Enqueuer:    enq,
		Now:         time.Now,
		parser:      cronlib.NewParser(cronlib.Minute | cronlib.Hour | cronlib.Dom | cronlib.Month | cronlib.Dow),
		lastFiredAt: make(map[string]time.Time),
	}
}

// Run blocks until ctx is cancelled, driving both loops on tickers.
func (s *Scheduler) Run(ctx context.Context) {
	cfg := s.Config.withDefaults()
	logger := s.logger()

	cronTick := time.NewTicker(cfg.CronInterval)
	reapTick := time.NewTicker(cfg.ReaperInterval)
	defer cronTick.Stop()
	defer reapTick.Stop()

	logger.InfoContext(ctx, "scheduler started",
		"cron_interval", cfg.CronInterval,
		"reaper_interval", cfg.ReaperInterval,
		"stuck_after", cfg.StuckAfter)

	for {
		select {
		case <-ctx.Done():
			logger.InfoContext(ctx, "scheduler stopped")
			return
		case <-cronTick.C:
			if err := s.fireDue(ctx); err != nil {
				logger.ErrorContext(ctx, "cron tick failed", "err", err)
			}
		case <-reapTick.C:
			if err := s.reapStuck(ctx, cfg.StuckAfter); err != nil {
				logger.ErrorContext(ctx, "reaper tick failed", "err", err)
			}
		}
	}
}

// fireDue scans every schedulable pipeline and enqueues a Run for any whose
// next firing time is in the past since the last evaluation. Idempotency
// is enforced at the Repo level via the (pipeline_id, idempotency_key)
// unique constraint.
func (s *Scheduler) fireDue(ctx context.Context) error {
	pipelines, err := s.Repo.ListSchedulablePipelines(ctx, "")
	if err != nil {
		return fmt.Errorf("list schedulable: %w", err)
	}
	now := s.now().UTC()

	for _, p := range pipelines {
		fireAt, ok := s.dueAt(p, now)
		if !ok {
			continue
		}
		if !s.markFired(p.ID, fireAt) {
			continue // already fired this tick within the process
		}
		key := idempotencyKey(p.ID, fireAt)
		run, err := s.Repo.CreateRun(ctx, &pipeline.Run{
			TenantID:       p.TenantID,
			PipelineID:     p.ID,
			Status:         pipeline.RunQueued,
			ScheduledAt:    fireAt,
			TriggeredBy:    "schedule",
			IdempotencyKey: key,
		})
		if err != nil {
			// Unique-violation = another scheduler beat us; that's fine.
			s.logger().DebugContext(ctx, "schedule create skipped", "pipeline", p.ID, "err", err)
			continue
		}
		if err := s.Enqueuer.EnqueuePipelineRun(ctx, worker.PipelineRunPayload{
			TenantID: p.TenantID, RunID: run.ID,
		}); err != nil {
			s.logger().WarnContext(ctx, "schedule enqueue failed", "pipeline", p.ID, "run", run.ID, "err", err)
		}
	}
	return nil
}

// dueAt returns the most recent firing time for p that is <= now and was not
// already fired in this process. Pipelines with bad cron expressions are
// skipped silently — validation belongs at create time, not in the loop.
func (s *Scheduler) dueAt(p *pipeline.Pipeline, now time.Time) (time.Time, bool) {
	if p.Schedule == nil || !p.Schedule.Enabled || p.Schedule.Cron == "" {
		return time.Time{}, false
	}
	loc := time.UTC
	if p.Schedule.Timezone != "" {
		if z, err := time.LoadLocation(p.Schedule.Timezone); err == nil {
			loc = z
		}
	}
	sched, err := s.parser.Parse(p.Schedule.Cron)
	if err != nil {
		return time.Time{}, false
	}
	// Anchor lookback to last fired (or 2*CronInterval ago to bound work).
	s.mu.Lock()
	last := s.lastFiredAt[p.ID]
	s.mu.Unlock()
	if last.IsZero() {
		last = now.Add(-2 * s.Config.withDefaults().CronInterval)
	}
	next := sched.Next(last.In(loc))
	if next.IsZero() || next.After(now) {
		return time.Time{}, false
	}
	return next.UTC(), true
}

// markFired records that we already fired this (pipeline, time) pair this
// process to prevent in-process double-firing on overlapping ticks. Returns
// true if the caller is the firstoner to claim it.
func (s *Scheduler) markFired(pipelineID string, at time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing := s.lastFiredAt[pipelineID]; !existing.Before(at) {
		return false
	}
	s.lastFiredAt[pipelineID] = at
	return true
}

// reapStuck marks running runs whose heartbeat (or started_at) is older than
// stuckAfter as failed. Best-effort: a runner that comes back to life sees
// the row already updated and stops cleanly.
func (s *Scheduler) reapStuck(ctx context.Context, stuckAfter time.Duration) error {
	cutoff := s.now().UTC().Add(-stuckAfter)
	stuck, err := s.Repo.ListStuckRuns(ctx, cutoff)
	if err != nil {
		return err
	}
	for _, r := range stuck {
		r.Status = pipeline.RunFailed
		if r.FinishedAt.IsZero() {
			r.FinishedAt = s.now().UTC()
		}
		if err := s.Repo.UpdateRun(ctx, r); err != nil {
			s.logger().WarnContext(ctx, "reap update failed", "run", r.ID, "err", err)
			continue
		}
		s.publish(ctx, events.Event{
			Type: events.RunFailed, Severity: events.SeverityError,
			TenantID: r.TenantID, ResourceType: "run", ResourceID: r.ID,
			Attributes: map[string]any{"reason": "stuck", "stuck_after_seconds": stuckAfter.Seconds()},
		})
		s.logger().InfoContext(ctx, "reaped stuck run", "run", r.ID, "tenant", r.TenantID)
	}
	return nil
}

func (s *Scheduler) publish(ctx context.Context, e events.Event) {
	if s.Events == nil {
		return
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = s.now().UTC()
	}
	s.Events.Publish(ctx, e)
}

func (s *Scheduler) logger() *slog.Logger {
	if s.Logger != nil {
		return s.Logger
	}
	return slog.Default()
}

func (s *Scheduler) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// idempotencyKey is sha256(pipeline_id ‖ scheduled_at) truncated. Stable so
// any scheduler instance computes the same key for the same (pipeline,
// time) pair, which is what makes dedup work.
func idempotencyKey(pipelineID string, at time.Time) string {
	h := sha256.New()
	_, _ = h.Write([]byte(pipelineID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(at.UTC().Format(time.RFC3339)))
	return hex.EncodeToString(h.Sum(nil))[:24]
}
