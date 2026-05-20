package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	cert "github.com/Satyaamm/plowered/internal/core/certification"
)

// CertificationStore is the Postgres-backed certification.Store. Rows
// are append-only; the service composes Approve/Reject/Revoke as new
// inserts that point at the same asset.
type CertificationStore struct {
	pool *pgxpool.Pool
}

func NewCertificationStore(p *pgxpool.Pool) *CertificationStore {
	return &CertificationStore{pool: p}
}

func (s *CertificationStore) Create(ctx context.Context, c *cert.Certification) (*cert.Certification, error) {
	if c == nil {
		return nil, errors.New("certification: nil row")
	}
	const q = `
		INSERT INTO asset_certifications
			(tenant_id, asset_id, status, proposed_by, reviewed_by, reviewed_at, justification, review_note)
		VALUES ($1::uuid, $2, $3, NULLIF($4,'')::uuid, NULLIF($5,'')::uuid, $6, $7, $8)
		RETURNING id::text, proposed_at`
	row := s.pool.QueryRow(ctx, q,
		c.TenantID, c.AssetID, string(c.Status),
		c.ProposedBy, c.ReviewedBy, c.ReviewedAt,
		c.Justification, c.ReviewNote,
	)
	if err := row.Scan(&c.ID, &c.ProposedAt); err != nil {
		return nil, fmt.Errorf("insert certification: %w", err)
	}
	return c, nil
}

func (s *CertificationStore) Get(ctx context.Context, tenantID, id string) (*cert.Certification, error) {
	const q = `
		SELECT id::text, tenant_id::text, asset_id, status,
		       COALESCE(proposed_by::text,''), proposed_at,
		       COALESCE(reviewed_by::text,''), reviewed_at,
		       justification, review_note
		  FROM asset_certifications
		 WHERE tenant_id = $1::uuid AND id = $2::uuid`
	return scanCert(s.pool.QueryRow(ctx, q, tenantID, id))
}

func (s *CertificationStore) Latest(ctx context.Context, tenantID, assetID string) (*cert.Certification, error) {
	const q = `
		SELECT id::text, tenant_id::text, asset_id, status,
		       COALESCE(proposed_by::text,''), proposed_at,
		       COALESCE(reviewed_by::text,''), reviewed_at,
		       justification, review_note
		  FROM asset_certifications
		 WHERE tenant_id = $1::uuid AND asset_id = $2
		 ORDER BY proposed_at DESC
		 LIMIT 1`
	c, err := scanCert(s.pool.QueryRow(ctx, q, tenantID, assetID))
	if errors.Is(err, cert.ErrNotFound) {
		return nil, nil
	}
	return c, err
}

func (s *CertificationStore) History(ctx context.Context, tenantID, assetID string) ([]*cert.Certification, error) {
	const q = `
		SELECT id::text, tenant_id::text, asset_id, status,
		       COALESCE(proposed_by::text,''), proposed_at,
		       COALESCE(reviewed_by::text,''), reviewed_at,
		       justification, review_note
		  FROM asset_certifications
		 WHERE tenant_id = $1::uuid AND asset_id = $2
		 ORDER BY proposed_at DESC`
	rows, err := s.pool.Query(ctx, q, tenantID, assetID)
	if err != nil {
		return nil, fmt.Errorf("list certifications: %w", err)
	}
	defer rows.Close()
	out := []*cert.Certification{}
	for rows.Next() {
		c, err := scanCert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *CertificationStore) ListPending(ctx context.Context, tenantID string, limit int) ([]*cert.Certification, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	// Latest-per-asset filtered to status=proposed. DISTINCT ON is the
	// concise Postgres idiom for "give me the most-recent row per
	// asset" without a subquery.
	const q = `
		WITH latest AS (
			SELECT DISTINCT ON (asset_id)
			       id, tenant_id, asset_id, status, proposed_by, proposed_at,
			       reviewed_by, reviewed_at, justification, review_note
			  FROM asset_certifications
			 WHERE tenant_id = $1::uuid
			 ORDER BY asset_id, proposed_at DESC
		)
		SELECT id::text, tenant_id::text, asset_id, status,
		       COALESCE(proposed_by::text,''), proposed_at,
		       COALESCE(reviewed_by::text,''), reviewed_at,
		       justification, review_note
		  FROM latest
		 WHERE status = 'proposed'
		 ORDER BY proposed_at DESC
		 LIMIT $2`
	rows, err := s.pool.Query(ctx, q, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("list pending: %w", err)
	}
	defer rows.Close()
	out := []*cert.Certification{}
	for rows.Next() {
		c, err := scanCert(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func scanCert(row pgx.Row) (*cert.Certification, error) {
	c := &cert.Certification{}
	var statusStr string
	var reviewedAt *time.Time
	err := row.Scan(
		&c.ID, &c.TenantID, &c.AssetID, &statusStr,
		&c.ProposedBy, &c.ProposedAt,
		&c.ReviewedBy, &reviewedAt,
		&c.Justification, &c.ReviewNote,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, cert.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan certification: %w", err)
	}
	c.Status = cert.Status(statusStr)
	c.ReviewedAt = reviewedAt
	return c, nil
}
