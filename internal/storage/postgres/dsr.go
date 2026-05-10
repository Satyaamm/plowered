package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/dsr"
)

// DSRStore is the Postgres-backed dsr.Repo. Like the legal_holds store,
// this depends on tenants(id) being populated; the user/tenant signup
// flow now writes those rows so the FK is real.
//
// `received_at` and `due_at` are stamped on Create (server-side, never
// client-supplied) so the 30-day GDPR clock is auditable from the row
// itself rather than reconstructed from log lines.
type DSRStore struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewDSRStore(pool *pgxpool.Pool) *DSRStore {
	return &DSRStore{pool: pool, now: time.Now}
}

func (s *DSRStore) Create(ctx context.Context, r *dsr.Request) (*dsr.Request, error) {
	if r == nil {
		return nil, errors.New("dsr: nil request")
	}
	cp := *r
	if cp.ReceivedAt.IsZero() {
		cp.ReceivedAt = s.now().UTC()
	}
	if cp.DueAt.IsZero() {
		cp.DueAt = cp.ReceivedAt.Add(dsr.SLA)
	}
	if cp.Status == "" {
		cp.Status = dsr.StatusReceived
	}
	const q = `
		INSERT INTO dsr_requests (
			tenant_id, subject_id, type, status,
			received_at, due_at, notes
		) VALUES ($1::uuid, $2, $3, $4, $5, $6, $7)
		RETURNING id::text`
	if err := s.pool.QueryRow(ctx, q,
		cp.TenantID, cp.SubjectID, string(cp.Type), string(cp.Status),
		cp.ReceivedAt, cp.DueAt, cp.Notes,
	).Scan(&cp.ID); err != nil {
		return nil, fmt.Errorf("dsr: create: %w", err)
	}
	return &cp, nil
}

func (s *DSRStore) Get(ctx context.Context, tenantID, id string) (*dsr.Request, error) {
	row := s.pool.QueryRow(ctx, selectDSRSQL+`
		WHERE tenant_id = $1::uuid AND id = $2::uuid`, tenantID, id)
	return scanDSR(row)
}

func (s *DSRStore) List(ctx context.Context, tenantID string) ([]*dsr.Request, error) {
	rows, err := s.pool.Query(ctx, selectDSRSQL+`
		WHERE tenant_id = $1::uuid
		 ORDER BY received_at DESC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("dsr: list: %w", err)
	}
	defer rows.Close()
	out := []*dsr.Request{}
	for rows.Next() {
		r, err := scanDSR(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *DSRStore) UpdateStatus(ctx context.Context, tenantID, id string, status dsr.Status, handledBy, artifactURN, notes string) error {
	completedAt := time.Time{}
	if status == dsr.StatusCompleted {
		completedAt = s.now().UTC()
	}
	var handledByArg any
	if handledBy != "" {
		handledByArg = handledBy
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE dsr_requests
		   SET status        = $3,
		       handled_by    = COALESCE($4::uuid, handled_by),
		       artifact_urn  = CASE WHEN $5 = '' THEN artifact_urn ELSE $5 END,
		       notes         = CASE WHEN $6 = '' THEN notes ELSE $6 END,
		       completed_at  = CASE WHEN $7::timestamptz = '0001-01-01 00:00:00+00'
		                            THEN completed_at ELSE $7 END
		 WHERE tenant_id = $1::uuid AND id = $2::uuid`,
		tenantID, id, string(status), handledByArg, artifactURN, notes, completedAt,
	)
	if err != nil {
		return fmt.Errorf("dsr: update status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return dsr.ErrNotFound
	}
	return nil
}

const selectDSRSQL = `
	SELECT id::text, tenant_id::text, subject_id, type, status,
	       received_at, due_at,
	       COALESCE(completed_at, '0001-01-01 00:00:00+00'::timestamptz),
	       COALESCE(handled_by::text, ''),
	       notes, artifact_urn
	  FROM dsr_requests`

func scanDSR(row rowScanner) (*dsr.Request, error) {
	var (
		r        dsr.Request
		typeStr  string
		statusStr string
	)
	if err := row.Scan(
		&r.ID, &r.TenantID, &r.SubjectID, &typeStr, &statusStr,
		&r.ReceivedAt, &r.DueAt, &r.CompletedAt,
		&r.HandledBy, &r.Notes, &r.ArtifactURN,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, dsr.ErrNotFound
		}
		return nil, err
	}
	r.Type = dsr.Type(typeStr)
	r.Status = dsr.Status(statusStr)
	return &r, nil
}
