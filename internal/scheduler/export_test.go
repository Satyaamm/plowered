package scheduler

import "context"

// FireDueForTest exposes fireDue to the _test package.
func FireDueForTest(s *Scheduler, ctx context.Context) error { return s.fireDue(ctx) }

// ReapForTest exposes reapStuck to the _test package using the configured
// StuckAfter (or the zero-value default).
func ReapForTest(s *Scheduler, ctx context.Context) error {
	cfg := s.Config.withDefaults()
	return s.reapStuck(ctx, cfg.StuckAfter)
}
