package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/legalhold"
)

// LegalHoldStore is the Postgres-backed legalhold.Repo. The legal_holds
// table FK-references tenants(id) and users(id), so this only works once
// the signup flow has populated those rows — see Step 1.1.
//
// The matching predicate (Hold.Matches) lives entirely in Go: we LOAD
// active holds for the tenant and walk them in process. A small per-tenant
// list size and the cache the relay's already keeping make this trivial;
// pushing the predicate into SQL would only buy something at thousands of
// concurrent holds, which is not our regime.
type LegalHoldStore struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewLegalHoldStore(pool *pgxpool.Pool) *LegalHoldStore {
	return &LegalHoldStore{pool: pool, now: time.Now}
}

func (s *LegalHoldStore) List(ctx context.Context, tenantID string) ([]legalhold.Hold, error) {
	const q = `
		SELECT id::text, tenant_id::text, matter, reason, scope,
		       issued_by::text, issued_at,
		       COALESCE(released_at, '0001-01-01 00:00:00+00'::timestamptz)
		  FROM legal_holds
		 WHERE tenant_id = $1::uuid
		 ORDER BY issued_at DESC`
	rows, err := s.pool.Query(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("legalhold: list: %w", err)
	}
	defer rows.Close()
	out := []legalhold.Hold{}
	for rows.Next() {
		h, err := scanHold(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func (s *LegalHoldStore) Issue(ctx context.Context, h *legalhold.Hold) (*legalhold.Hold, error) {
	if h == nil {
		return nil, errors.New("legalhold: nil hold")
	}
	cp := *h
	if cp.IssuedAt.IsZero() {
		cp.IssuedAt = s.now().UTC()
	}
	scope, _ := json.Marshal(cp.Scope)
	if len(scope) == 0 || string(scope) == "null" {
		scope = []byte(`{}`)
	}
	const q = `
		INSERT INTO legal_holds (tenant_id, matter, reason, scope, issued_by, issued_at)
		VALUES ($1::uuid, $2, $3, $4::jsonb, $5::uuid, $6)
		RETURNING id::text`
	if err := s.pool.QueryRow(ctx, q,
		cp.TenantID, cp.Matter, cp.Reason, scope, cp.IssuedBy, cp.IssuedAt,
	).Scan(&cp.ID); err != nil {
		return nil, fmt.Errorf("legalhold: issue: %w", err)
	}
	return &cp, nil
}

func (s *LegalHoldStore) Release(ctx context.Context, tenantID, holdID, releasedBy string, at time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE legal_holds
		   SET released_at = $3, released_by = $4::uuid
		 WHERE tenant_id = $1::uuid AND id = $2::uuid AND released_at IS NULL`,
		tenantID, holdID, at, releasedBy,
	)
	if err != nil {
		return fmt.Errorf("legalhold: release: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errors.New("legalhold: not found or already released")
	}
	return nil
}

func (s *LegalHoldStore) Check(ctx context.Context, tenantID, resourceType, resourceID string, tags []string) (*legalhold.Hold, error) {
	// Only need active holds — released ones don't gate.
	const q = `
		SELECT id::text, tenant_id::text, matter, reason, scope,
		       issued_by::text, issued_at,
		       COALESCE(released_at, '0001-01-01 00:00:00+00'::timestamptz)
		  FROM legal_holds
		 WHERE tenant_id = $1::uuid AND released_at IS NULL`
	rows, err := s.pool.Query(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("legalhold: check: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		h, err := scanHold(rows)
		if err != nil {
			return nil, err
		}
		if h.Matches(resourceType, resourceID, tags) {
			cp := h
			return &cp, legalhold.ErrHeld
		}
	}
	return nil, rows.Err()
}

func scanHold(row rowScanner) (legalhold.Hold, error) {
	var (
		h     legalhold.Hold
		scope []byte
	)
	if err := row.Scan(
		&h.ID, &h.TenantID, &h.Matter, &h.Reason, &scope,
		&h.IssuedBy, &h.IssuedAt, &h.ReleasedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return h, errors.New("legalhold: not found")
		}
		return h, err
	}
	if len(scope) > 0 {
		_ = json.Unmarshal(scope, &h.Scope)
	}
	return h, nil
}
