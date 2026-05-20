package cost

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/Satyaamm/plowered/internal/core/events"
)

// Watcher polls per-tenant budgets and fires events when the rolling
// 30-day spend crosses warn_at_pct or hard_at_pct. Deduplicates by
// updating last_warned_at / last_hard_at so the same tenant doesn't
// generate an alert every poll.
//
// Owned by cmd lifecycle; call Start(ctx) once.
type Watcher struct {
	Budgets  BudgetStore
	Tenants  TenantLister
	Events   events.Bus
	Interval time.Duration
	Logger   *slog.Logger
}

// TenantLister yields the tenants the watcher should check. Backed by
// SELECT DISTINCT tenant_id FROM cost_records — same approach as the
// contract runner so empty tenants cost nothing.
type TenantLister interface {
	TenantsWithCost(ctx context.Context) ([]string, error)
}

func (w *Watcher) Start(ctx context.Context) {
	if w.Budgets == nil || w.Tenants == nil {
		return
	}
	if w.Interval <= 0 {
		w.Interval = 15 * time.Minute
	}
	go w.loop(ctx)
}

func (w *Watcher) loop(ctx context.Context) {
	t := time.NewTicker(w.Interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.runOnce(ctx)
		}
	}
}

func (w *Watcher) runOnce(ctx context.Context) {
	tenants, err := w.Tenants.TenantsWithCost(ctx)
	if err != nil {
		w.logger().WarnContext(ctx, "cost watcher: list tenants", "err", err)
		return
	}
	for _, t := range tenants {
		if ctx.Err() != nil {
			return
		}
		w.checkOne(ctx, t)
	}
}

func (w *Watcher) checkOne(ctx context.Context, tenantID string) {
	b, err := w.Budgets.GetBudget(ctx, tenantID)
	if err != nil || b == nil || b.MonthlyUSD == nil {
		return
	}
	total, err := w.Budgets.RollingTotal(ctx, tenantID, 30)
	if err != nil {
		w.logger().WarnContext(ctx, "cost watcher: rolling total", "tenant", tenantID, "err", err)
		return
	}
	pct := (total / *b.MonthlyUSD) * 100.0
	now := time.Now().UTC()

	// HARD breach takes precedence — emit critical and skip the warn
	// emit for the same poll.
	if pct >= float64(b.HardAtPct) {
		if shouldFire(b.LastHardAt, now, 24*time.Hour) {
			w.publish(ctx, tenantID, b, total, pct, "hard", events.SeverityCritical)
			_ = w.Budgets.MarkHard(ctx, tenantID, now)
		}
		return
	}
	if pct >= float64(b.WarnAtPct) {
		if shouldFire(b.LastWarnedAt, now, 24*time.Hour) {
			w.publish(ctx, tenantID, b, total, pct, "warn", events.SeverityWarning)
			_ = w.Budgets.MarkWarned(ctx, tenantID, now)
		}
	}
}

// shouldFire dedupes alerts within a cooldown window. nil last-fire ⇒
// always fire; otherwise fire only if cooldown has elapsed.
func shouldFire(last *time.Time, now time.Time, cooldown time.Duration) bool {
	if last == nil {
		return true
	}
	return now.Sub(*last) >= cooldown
}

func (w *Watcher) publish(ctx context.Context, tenantID string, b *Budget, total, pct float64, level string, sev events.Severity) {
	if w.Events == nil {
		return
	}
	w.Events.Publish(ctx, events.Event{
		Type:         events.CheckFailed,
		Severity:     sev,
		TenantID:     tenantID,
		ResourceType: "cost_budget",
		ResourceID:   tenantID,
		Attributes: map[string]any{
			"level":         level,
			"monthly_usd":   *b.MonthlyUSD,
			"observed_usd":  total,
			"pct_of_budget": pct,
			"warn_at_pct":   b.WarnAtPct,
			"hard_at_pct":   b.HardAtPct,
			"message":       fmt.Sprintf("monthly spend at %.1f%% of $%.2f budget", pct, *b.MonthlyUSD),
		},
		OccurredAt: time.Now().UTC(),
	})
}

func (w *Watcher) logger() *slog.Logger {
	if w.Logger != nil {
		return w.Logger
	}
	return slog.Default()
}
