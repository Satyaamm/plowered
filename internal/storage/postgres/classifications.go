package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ClassificationStore writes auto-detected classifications to the
// public.data_classifications table. Rows are tenant-scoped and
// uniquely keyed on (asset_id, classification) — the upsert dedupes
// repeated runs of the classifier.
type ClassificationStore struct {
	pool *pgxpool.Pool
}

func NewClassificationStore(p *pgxpool.Pool) *ClassificationStore {
	return &ClassificationStore{pool: p}
}

// Apply records `classification` against `assetID`. appliedBy can be
// empty for system-driven classification runs.
func (s *ClassificationStore) Apply(ctx context.Context, tenantID, assetID, classification, appliedBy string) error {
	const q = `
		INSERT INTO data_classifications (asset_id, tenant_id, classification, applied_by)
		VALUES ($1::uuid, $2::uuid, $3, NULLIF($4,'')::uuid)
		ON CONFLICT (asset_id, classification)
		DO UPDATE SET applied_by = COALESCE(EXCLUDED.applied_by, data_classifications.applied_by),
		              applied_at = now()`
	if _, err := s.pool.Exec(ctx, q, assetID, tenantID, classification, appliedBy); err != nil {
		return fmt.Errorf("apply classification: %w", err)
	}
	return nil
}

// ListByAsset returns the classifications stamped on a single asset.
func (s *ClassificationStore) ListByAsset(ctx context.Context, tenantID, assetID string) ([]string, error) {
	const q = `
		SELECT classification FROM data_classifications
		 WHERE tenant_id = $1::uuid AND asset_id = $2::uuid
		 ORDER BY classification`
	rows, err := s.pool.Query(ctx, q, tenantID, assetID)
	if err != nil {
		return nil, fmt.Errorf("list classifications: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ListByTenant returns every (asset_id, classification) pair for a
// tenant. Useful for the admin "who carries PCI?" type views.
func (s *ClassificationStore) ListByTenant(ctx context.Context, tenantID string) ([]ClassificationRow, error) {
	const q = `
		SELECT asset_id::text, classification, applied_at
		  FROM data_classifications
		 WHERE tenant_id = $1::uuid
		 ORDER BY classification, applied_at DESC`
	rows, err := s.pool.Query(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list classifications: %w", err)
	}
	defer rows.Close()
	var out []ClassificationRow
	for rows.Next() {
		var r ClassificationRow
		if err := rows.Scan(&r.AssetID, &r.Classification, &r.AppliedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ClassificationRow is the API-friendly view of one row.
type ClassificationRow struct {
	AssetID        string
	Classification string
	AppliedAt      time.Time
}
