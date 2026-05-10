package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/deleted"
)

// DeletedStore is the Postgres-backed deleted.Repo. Schema lives in
// migration 0003 (deleted_records).
type DeletedStore struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewDeletedStore(pool *pgxpool.Pool) *DeletedStore {
	return &DeletedStore{pool: pool, now: time.Now}
}

func (s *DeletedStore) Capture(ctx context.Context, r *deleted.Record) (*deleted.Record, error) {
	if r == nil {
		return nil, errors.New("deleted: nil record")
	}
	cp := *r
	if cp.DeletedAt.IsZero() {
		cp.DeletedAt = s.now().UTC()
	}
	if cp.DeletionReason == "" {
		cp.DeletionReason = deleted.ReasonUserAction
	}
	if cp.DeletedKind == "" {
		cp.DeletedKind = "user"
	}
	payloadJSON, _ := json.Marshal(cp.Payload)

	var parent any
	if cp.ParentTombstoneID != "" {
		parent = cp.ParentTombstoneID
	}

	const q = `
		INSERT INTO deleted_records (
			id, tenant_id, resource_type, resource_id, payload,
			deleted_by, deleted_kind, deletion_reason, request_id,
			parent_tombstone_id, deleted_at
		) VALUES (
			COALESCE(NULLIF($1,'')::uuid, gen_random_uuid()),
			$2, $3, $4, $5, $6, $7, $8, $9, $10::uuid, $11
		) RETURNING id::text`
	if err := s.pool.QueryRow(ctx, q,
		cp.ID, cp.TenantID, cp.ResourceType, cp.ResourceID, payloadJSON,
		cp.DeletedBy, cp.DeletedKind, string(cp.DeletionReason), cp.RequestID,
		parent, cp.DeletedAt,
	).Scan(&cp.ID); err != nil {
		return nil, fmt.Errorf("deleted: capture: %w", err)
	}
	return &cp, nil
}

func (s *DeletedStore) Get(ctx context.Context, tenantID, id string) (*deleted.Record, error) {
	row := s.pool.QueryRow(ctx, selectDeletedSQL+`
		WHERE tenant_id = $1 AND id = $2::uuid`, tenantID, id)
	return scanDeleted(row)
}

func (s *DeletedStore) List(ctx context.Context, tenantID string, opts deleted.ListOptions) ([]*deleted.Record, error) {
	limit := opts.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	q := selectDeletedSQL + ` WHERE tenant_id = $1`
	args := []any{tenantID}
	if opts.ResourceType != "" {
		q += fmt.Sprintf(` AND resource_type = $%d`, len(args)+1)
		args = append(args, opts.ResourceType)
	}
	if !opts.IncludeRestored {
		q += ` AND restored_at IS NULL`
	}
	if !opts.IncludePurged {
		q += ` AND purged_at IS NULL`
	}
	q += fmt.Sprintf(` ORDER BY deleted_at DESC LIMIT %d`, limit)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("deleted: list: %w", err)
	}
	defer rows.Close()
	out := make([]*deleted.Record, 0, limit)
	for rows.Next() {
		r, err := scanDeleted(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *DeletedStore) MarkRestored(ctx context.Context, tenantID, id, restoredBy string, at time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE deleted_records
		   SET restored_at = $3, restored_by = $4
		 WHERE tenant_id = $1 AND id = $2::uuid
		   AND purged_at IS NULL`,
		tenantID, id, at, restoredBy,
	)
	if err != nil {
		return fmt.Errorf("deleted: mark restored: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return deleted.ErrNotFound
	}
	return nil
}

func (s *DeletedStore) MarkPurged(ctx context.Context, tenantID, id, purgedBy string, at time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE deleted_records
		   SET purged_at = $3, purged_by = $4
		 WHERE tenant_id = $1 AND id = $2::uuid`,
		tenantID, id, at, purgedBy,
	)
	if err != nil {
		return fmt.Errorf("deleted: mark purged: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return deleted.ErrNotFound
	}
	return nil
}

const selectDeletedSQL = `
	SELECT id::text, tenant_id, resource_type, resource_id, payload,
	       deleted_by, deleted_kind, deletion_reason, request_id,
	       COALESCE(parent_tombstone_id::text, ''),
	       deleted_at,
	       COALESCE(restored_at, '0001-01-01 00:00:00+00'::timestamptz),
	       restored_by,
	       COALESCE(purged_at, '0001-01-01 00:00:00+00'::timestamptz),
	       purged_by
	  FROM deleted_records`

func scanDeleted(row rowScanner) (*deleted.Record, error) {
	var (
		r           deleted.Record
		payloadRaw  []byte
		reason      string
	)
	err := row.Scan(
		&r.ID, &r.TenantID, &r.ResourceType, &r.ResourceID, &payloadRaw,
		&r.DeletedBy, &r.DeletedKind, &reason, &r.RequestID,
		&r.ParentTombstoneID, &r.DeletedAt,
		&r.RestoredAt, &r.RestoredBy, &r.PurgedAt, &r.PurgedBy,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, deleted.ErrNotFound
		}
		return nil, err
	}
	r.DeletionReason = deleted.Reason(reason)
	if len(payloadRaw) > 0 {
		_ = json.Unmarshal(payloadRaw, &r.Payload)
	}
	return &r, nil
}
