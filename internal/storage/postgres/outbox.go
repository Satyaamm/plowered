package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/outbox"
)

// OutboxStore is the Postgres-backed Writer + Reader for the outbox
// pattern. Writes MUST run inside the caller's transaction (use the
// pgxpool.Tx form via WriteTx); reads are batched with FOR UPDATE SKIP
// LOCKED so multiple relay replicas safely race over the same rows.
type OutboxStore struct {
	pool *pgxpool.Pool
}

func NewOutboxStore(pool *pgxpool.Pool) *OutboxStore {
	return &OutboxStore{pool: pool}
}

// Write inserts a single outbox row with its own implicit transaction.
// In production, prefer the pattern: Begin TX → mutate domain row →
// Write outbox row → Commit. Writing on the pool here is safe for
// publishers like the audit middleware whose mutation is also a single
// statement.
func (s *OutboxStore) Write(ctx context.Context, e *outbox.Event) error {
	if e == nil {
		return fmt.Errorf("outbox: nil event")
	}
	payload, _ := json.Marshal(e.Payload)
	occurred := e.OccurredAt
	if occurred.IsZero() {
		occurred = time.Now().UTC()
	}
	const q = `
		INSERT INTO outbox (
			tenant_id, aggregate_type, aggregate_id, event_type,
			payload, occurred_at
		) VALUES ($1::uuid, $2, $3, $4, $5::jsonb, $6)
		RETURNING id`
	return s.pool.QueryRow(ctx, q,
		e.TenantID, e.AggregateType, e.AggregateID, e.EventType,
		payload, occurred,
	).Scan(&e.ID)
}

// NextBatch fetches up to `limit` unprocessed rows in commit order.
// FOR UPDATE SKIP LOCKED makes this a safe leader-electable read — two
// relay replicas naturally split the work without explicit coordination.
func (s *OutboxStore) NextBatch(ctx context.Context, limit int) ([]*outbox.Event, error) {
	const q = `
		SELECT id, tenant_id::text, aggregate_type, aggregate_id, event_type,
		       payload, occurred_at, process_attempts
		  FROM outbox
		 WHERE processed_at IS NULL
		 ORDER BY occurred_at ASC
		 LIMIT $1
		 FOR UPDATE SKIP LOCKED`
	rows, err := s.pool.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("outbox: next batch: %w", err)
	}
	defer rows.Close()
	out := make([]*outbox.Event, 0, limit)
	for rows.Next() {
		var (
			ev      outbox.Event
			payload []byte
		)
		if err := rows.Scan(
			&ev.ID, &ev.TenantID, &ev.AggregateType, &ev.AggregateID,
			&ev.EventType, &payload, &ev.OccurredAt, &ev.Attempts,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(payload, &ev.Payload)
		out = append(out, &ev)
	}
	return out, rows.Err()
}

func (s *OutboxStore) MarkProcessed(ctx context.Context, ids []int64, at time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE outbox
		   SET processed_at = $2
		 WHERE id = ANY($1::bigint[])`, ids, at)
	if err != nil {
		return fmt.Errorf("outbox: mark processed: %w", err)
	}
	return nil
}

func (s *OutboxStore) MarkFailed(ctx context.Context, id int64, errMsg string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE outbox
		   SET process_attempts = process_attempts + 1,
		       last_error = $2
		 WHERE id = $1`, id, errMsg)
	if err != nil {
		return fmt.Errorf("outbox: mark failed: %w", err)
	}
	return nil
}
