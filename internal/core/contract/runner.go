package contract

import (
	"context"
	"log/slog"
	"time"
)

// TenantLister yields the set of tenants the runner should evaluate
// each cycle. Backed by SELECT DISTINCT tenant_id FROM data_contracts
// in the Postgres impl — we only walk tenants that have contracts at
// all, so an empty workspace costs nothing.
type TenantLister interface {
	TenantsWithContracts(ctx context.Context) ([]string, error)
}

// Runner periodically evaluates every active contract across every
// tenant. Owned by the cmd binary's lifecycle — call Start, then Stop
// (or cancel its context) on shutdown.
type Runner struct {
	Service  *Service
	Tenants  TenantLister
	Interval time.Duration
	Logger   *slog.Logger
}

// Start kicks off the periodic loop on a goroutine. The first tick
// fires after Interval (not immediately) so a fresh boot doesn't
// thrash the warehouse before the API is even serving requests.
func (r *Runner) Start(ctx context.Context) {
	if r.Service == nil || r.Tenants == nil {
		return
	}
	if r.Interval <= 0 {
		r.Interval = 5 * time.Minute
	}
	go r.loop(ctx)
}

func (r *Runner) loop(ctx context.Context) {
	t := time.NewTicker(r.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.runOnce(ctx)
		}
	}
}

func (r *Runner) runOnce(ctx context.Context) {
	tenants, err := r.Tenants.TenantsWithContracts(ctx)
	if err != nil {
		r.logger().WarnContext(ctx, "contract runner: list tenants", "err", err)
		return
	}
	for _, t := range tenants {
		if ctx.Err() != nil {
			return
		}
		count, err := r.Service.EvaluateAll(ctx, t)
		if err != nil {
			r.logger().WarnContext(ctx, "contract runner: evaluate", "tenant", t, "err", err)
			continue
		}
		if count > 0 {
			r.logger().InfoContext(ctx, "contract runner: breaches detected",
				"tenant", t, "count", count)
		}
	}
}

func (r *Runner) logger() *slog.Logger {
	if r.Logger != nil {
		return r.Logger
	}
	return slog.Default()
}
