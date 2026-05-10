package quality

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// RunOptions tunes a single check execution. Zero values fall back to the
// runner-level defaults configured on Scheduler.
type RunOptions struct {
	// Timeout caps total wall-clock time for the check. Cancellation
	// propagates to the DataSource via context.
	Timeout time.Duration

	// SamplePercent, if > 0 and < 100, asks the DataSource to evaluate the
	// check against a server-side sample rather than the full table. The
	// DataSource decides how to honor it (TABLESAMPLE on Postgres,
	// SAMPLE on Snowflake, TABLESAMPLE SYSTEM on BigQuery). Checks that
	// can't be sampled (custom_sql) ignore this field.
	SamplePercent float64

	// Source is an opaque key naming the customer datasource — typically
	// "<tenant>:<connection_id>". Used to enforce per-source concurrency.
	Source string
}

// SchedulerConfig controls the global behavior of a Scheduler.
type SchedulerConfig struct {
	DefaultTimeout       time.Duration // 0 → 5m
	MaxTimeout           time.Duration // 0 → 60m
	DefaultSamplePercent float64       // 0 → no sampling
	MaxConcurrentPerSource int         // 0 → 3
}

// Scheduler turns a synchronous Runner into a bounded, cancellable executor.
// One Scheduler is shared across workers; per-source semaphores keep a
// noisy customer DB from being hammered.
//
// Scheduler is intentionally transport-agnostic — the Asynq worker, the
// in-process bus, and the embedded mode all call Schedule.
type Scheduler struct {
	runner *Runner
	cfg    SchedulerConfig

	mu         sync.Mutex
	semaphores map[string]chan struct{}
}

// NewScheduler builds a Scheduler over an existing Runner.
func NewScheduler(runner *Runner, cfg SchedulerConfig) *Scheduler {
	if runner == nil {
		runner = NewRunner()
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 5 * time.Minute
	}
	if cfg.MaxTimeout <= 0 {
		cfg.MaxTimeout = 60 * time.Minute
	}
	if cfg.MaxConcurrentPerSource <= 0 {
		cfg.MaxConcurrentPerSource = 3
	}
	return &Scheduler{
		runner:     runner,
		cfg:        cfg,
		semaphores: make(map[string]chan struct{}),
	}
}

// SamplingDataSource is implemented by DataSources that can apply a
// server-side sample. Sources that don't implement it fall back to the
// non-sampled path.
type SamplingDataSource interface {
	DataSource
	WithSample(percent float64) DataSource
}

// Schedule runs check against ds, honoring the configured timeouts and
// concurrency caps. The returned CheckRun is always non-nil; on context
// cancellation or timeout it carries OutcomeError with a descriptive
// Diagnostic.
func (s *Scheduler) Schedule(ctx context.Context, check Check, ds DataSource, opts RunOptions) *CheckRun {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = s.cfg.DefaultTimeout
	}
	if timeout > s.cfg.MaxTimeout {
		timeout = s.cfg.MaxTimeout
	}

	sample := opts.SamplePercent
	if sample <= 0 {
		sample = s.cfg.DefaultSamplePercent
	}
	if sample > 0 && sample < 100 && check.Type != CheckCustomSQL {
		if sds, ok := ds.(SamplingDataSource); ok {
			ds = sds.WithSample(sample)
		}
	}

	if opts.Source != "" {
		release, err := s.acquire(ctx, opts.Source)
		if err != nil {
			return s.errorRun(check, fmt.Errorf("acquire %q: %w", opts.Source, err))
		}
		defer release()
	}

	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cr := s.runner.Run(runCtx, check, ds)

	// Surface deadline-exceeded explicitly — the underlying driver may
	// already have written an Outcome based on its own error reporting,
	// but a vanilla `context deadline exceeded` is more useful.
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) && cr.Outcome != OutcomeFail {
		cr.Outcome = OutcomeError
		cr.Diagnostic = fmt.Sprintf("check timed out after %s", timeout)
	}
	return cr
}

func (s *Scheduler) errorRun(check Check, err error) *CheckRun {
	now := s.runner.Now
	if now == nil {
		now = time.Now
	}
	t := now().UTC()
	return &CheckRun{
		ID:         newID(),
		TenantID:   check.TenantID,
		CheckID:    check.ID,
		AssetID:    check.AssetID,
		Outcome:    OutcomeError,
		Diagnostic: err.Error(),
		Severity:   check.Severity,
		StartedAt:  t,
		FinishedAt: t,
	}
}

func (s *Scheduler) acquire(ctx context.Context, source string) (release func(), err error) {
	s.mu.Lock()
	sem, ok := s.semaphores[source]
	if !ok {
		sem = make(chan struct{}, s.cfg.MaxConcurrentPerSource)
		s.semaphores[source] = sem
	}
	s.mu.Unlock()

	select {
	case sem <- struct{}{}:
		return func() { <-sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
