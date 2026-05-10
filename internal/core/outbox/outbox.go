// Package outbox implements the transactional-outbox pattern for
// reliable event delivery from Postgres to an external bus (NATS today,
// Kafka tomorrow). State changes write a row in the same TX as the
// domain mutation; a separate relay loop reads `processed_at IS NULL`
// rows in commit order and publishes them.
//
// Why outbox instead of dual-write or 2PC:
//   - Dual-write (DB + bus from the same handler) leaves stuck events on
//     a process crash between the two writes.
//   - 2PC across Postgres + NATS is operationally fragile and most cloud
//     brokers don't support it.
// The outbox is the boring, correct answer.
//
// Schema lives in migration 0004_enterprise (`outbox` table).
package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// Event is one row in the outbox table.
type Event struct {
	ID             int64
	TenantID       string
	AggregateType  string // "pipeline_run" | "asset" | ...
	AggregateID    string
	EventType      string // "pipeline_run.failed" | "asset.created"
	Payload        map[string]any
	OccurredAt     time.Time
	ProcessedAt    time.Time
	Attempts       int
	LastError      string
}

// Writer persists outbox rows. Implementations MUST run within the
// caller's transaction so either both the domain row and the outbox row
// commit, or neither does.
type Writer interface {
	Write(ctx context.Context, e *Event) error
}

// Reader returns up to `limit` unprocessed rows in occurred_at order, and
// MUST surface read-skew safe rows (e.g. via FOR UPDATE SKIP LOCKED) so
// multiple relay replicas don't double-publish.
type Reader interface {
	NextBatch(ctx context.Context, limit int) ([]*Event, error)
	MarkProcessed(ctx context.Context, ids []int64, at time.Time) error
	MarkFailed(ctx context.Context, id int64, err string) error
}

// Publisher forwards an event to an external bus. Returning an error
// causes the relay to MarkFailed and retry on a future tick — the row
// stays in the outbox until publish succeeds.
type Publisher interface {
	Publish(ctx context.Context, e *Event) error
}

// LogPublisher writes events to slog. Useful for early bring-up and as a
// safety net during NATS outages — the audit-from-logs trail still has
// the data even if the bus is down.
type LogPublisher struct {
	Logger *slog.Logger
}

func (p LogPublisher) Publish(_ context.Context, e *Event) error {
	if p.Logger == nil {
		return errors.New("outbox: LogPublisher missing logger")
	}
	body, _ := json.Marshal(e.Payload)
	p.Logger.Info("outbox.publish",
		"id", e.ID,
		"tenant_id", e.TenantID,
		"event_type", e.EventType,
		"aggregate_type", e.AggregateType,
		"aggregate_id", e.AggregateID,
		"payload", string(body),
	)
	return nil
}

// MultiPublisher fans out a single event to several bus implementations,
// useful when migrating from one broker to another. ALL must succeed for
// the row to be marked processed; partial success retries everyone (so
// downstream consumers MUST be idempotent — same as any at-least-once bus).
type MultiPublisher struct {
	Publishers []Publisher
}

func (m MultiPublisher) Publish(ctx context.Context, e *Event) error {
	var errs []error
	for _, p := range m.Publishers {
		if err := p.Publish(ctx, e); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Config tunes the relay. Defaults are sane for "first live"; production
// dials BatchSize up and TickInterval down.
type Config struct {
	BatchSize    int
	TickInterval time.Duration
	MaxAttempts  int
}

func (c Config) withDefaults() Config {
	if c.BatchSize <= 0 {
		c.BatchSize = 100
	}
	if c.TickInterval <= 0 {
		c.TickInterval = 2 * time.Second
	}
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 8
	}
	return c
}

// Relay polls the outbox and forwards rows to the publisher. Run in a
// goroutine; stop by cancelling the context.
type Relay struct {
	Reader    Reader
	Publisher Publisher
	Logger    *slog.Logger
	Cfg       Config
}

// Run blocks until ctx is cancelled. Errors are logged and retried — the
// loop never exits on a transient publish failure.
func (r Relay) Run(ctx context.Context) {
	cfg := r.Cfg.withDefaults()
	logger := r.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if r.Reader == nil || r.Publisher == nil {
		logger.Warn("outbox: relay disabled — reader or publisher missing")
		return
	}
	logger.Info("outbox: relay started",
		"batch_size", cfg.BatchSize, "tick", cfg.TickInterval.String())
	t := time.NewTicker(cfg.TickInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			logger.Info("outbox: relay stopping")
			return
		case <-t.C:
			r.tick(ctx, cfg, logger)
		}
	}
}

func (r Relay) tick(ctx context.Context, cfg Config, logger *slog.Logger) {
	batch, err := r.Reader.NextBatch(ctx, cfg.BatchSize)
	if err != nil {
		logger.Warn("outbox: read batch", "err", err)
		return
	}
	if len(batch) == 0 {
		return
	}
	processed := make([]int64, 0, len(batch))
	for _, e := range batch {
		if err := r.Publisher.Publish(ctx, e); err != nil {
			logger.Warn("outbox: publish failed",
				"id", e.ID, "attempts", e.Attempts, "err", err)
			_ = r.Reader.MarkFailed(ctx, e.ID, err.Error())
			continue
		}
		processed = append(processed, e.ID)
	}
	if len(processed) > 0 {
		if err := r.Reader.MarkProcessed(ctx, processed, time.Now().UTC()); err != nil {
			logger.Warn("outbox: mark processed", "err", err)
		}
	}
}

// MemoryStore is an in-process Writer + Reader pair for tests. It is NOT
// safe for production — outbox semantics rely on the same TX as the
// domain mutation, which is impossible in memory.
type MemoryStore struct {
	mu       sync.Mutex
	nextID   int64
	rows     []*Event
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{} }

func (m *MemoryStore) Write(_ context.Context, e *Event) error {
	if e == nil {
		return errors.New("outbox: nil event")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	cp := *e
	cp.ID = m.nextID
	if cp.OccurredAt.IsZero() {
		cp.OccurredAt = time.Now().UTC()
	}
	m.rows = append(m.rows, &cp)
	return nil
}

func (m *MemoryStore) NextBatch(_ context.Context, limit int) ([]*Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Event, 0, limit)
	for _, r := range m.rows {
		if !r.ProcessedAt.IsZero() {
			continue
		}
		out = append(out, r)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (m *MemoryStore) MarkProcessed(_ context.Context, ids []int64, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		for _, id := range ids {
			if r.ID == id {
				r.ProcessedAt = at
			}
		}
	}
	return nil
}

func (m *MemoryStore) MarkFailed(_ context.Context, id int64, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if r.ID == id {
			r.Attempts++
			r.LastError = errMsg
			return nil
		}
	}
	return errors.New("outbox: not found")
}

// All returns a snapshot for tests.
func (m *MemoryStore) All() []*Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Event, len(m.rows))
	for i, r := range m.rows {
		cp := *r
		out[i] = &cp
	}
	return out
}
