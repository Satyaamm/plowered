package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/vectorstore"
)

// VectorStoreConfigStore is the Postgres-backed Repo for
// vector_store_configs. Mirrors AIProviderStore's shape so the two
// configuration surfaces stay symmetric.
type VectorStoreConfigStore struct {
	pool *pgxpool.Pool
}

func NewVectorStoreConfigStore(p *pgxpool.Pool) *VectorStoreConfigStore {
	return &VectorStoreConfigStore{pool: p}
}

func (s *VectorStoreConfigStore) Create(ctx context.Context, c *vectorstore.Config) (*vectorstore.Config, error) {
	const q = `
		INSERT INTO vector_store_configs
		    (tenant_id, kind, name, endpoint, index_name, class_name,
		     collection, dimension, secret_urn, is_primary)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id::text, created_at, updated_at`
	if err := s.pool.QueryRow(ctx, q,
		c.TenantID, string(c.Kind), c.Name, c.Endpoint, c.IndexName, c.ClassName,
		c.Collection, c.Dimension, c.SecretURN, c.IsPrimary,
	).Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, fmt.Errorf("create vector_store_config: %w", err)
	}
	return c, nil
}

func (s *VectorStoreConfigStore) Get(ctx context.Context, tenantID, id string) (*vectorstore.Config, error) {
	const q = `
		SELECT id::text, tenant_id, kind, name, endpoint, index_name,
		       class_name, collection, dimension, secret_urn, is_primary,
		       COALESCE(last_tested_at, '0001-01-01 00:00:00+00'::timestamptz),
		       last_test_ok, last_test_error, created_at, updated_at
		  FROM vector_store_configs
		 WHERE tenant_id = $1 AND id = $2::uuid`
	return scanVectorStore(s.pool.QueryRow(ctx, q, tenantID, id))
}

func (s *VectorStoreConfigStore) List(ctx context.Context, tenantID string) ([]*vectorstore.Config, error) {
	const q = `
		SELECT id::text, tenant_id, kind, name, endpoint, index_name,
		       class_name, collection, dimension, secret_urn, is_primary,
		       COALESCE(last_tested_at, '0001-01-01 00:00:00+00'::timestamptz),
		       last_test_ok, last_test_error, created_at, updated_at
		  FROM vector_store_configs
		 WHERE tenant_id = $1
		 ORDER BY created_at DESC`
	rows, err := s.pool.Query(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list vector_store_configs: %w", err)
	}
	defer rows.Close()
	out := []*vectorstore.Config{}
	for rows.Next() {
		c, err := scanVectorStore(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *VectorStoreConfigStore) Update(ctx context.Context, c *vectorstore.Config) (*vectorstore.Config, error) {
	const q = `
		UPDATE vector_store_configs
		   SET name = $3, endpoint = $4, index_name = $5,
		       class_name = $6, collection = $7, dimension = $8,
		       updated_at = now()
		 WHERE tenant_id = $1 AND id = $2::uuid
		RETURNING updated_at`
	if err := s.pool.QueryRow(ctx, q,
		c.TenantID, c.ID, c.Name, c.Endpoint, c.IndexName,
		c.ClassName, c.Collection, c.Dimension,
	).Scan(&c.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, vectorstore.ErrNotFound
		}
		return nil, fmt.Errorf("update vector_store_config: %w", err)
	}
	return c, nil
}

func (s *VectorStoreConfigStore) Delete(ctx context.Context, tenantID, id string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM vector_store_configs WHERE tenant_id = $1 AND id = $2::uuid`,
		tenantID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return vectorstore.ErrNotFound
	}
	return nil
}

func (s *VectorStoreConfigStore) MarkTested(ctx context.Context, tenantID, id string, ok bool, errMsg string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE vector_store_configs
		   SET last_tested_at = now(),
		       last_test_ok = $3,
		       last_test_error = $4
		 WHERE tenant_id = $1 AND id = $2::uuid`,
		tenantID, id, ok, errMsg)
	return err
}

func (s *VectorStoreConfigStore) SetPrimary(ctx context.Context, tenantID, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		UPDATE vector_store_configs SET is_primary = FALSE
		 WHERE tenant_id = $1 AND id <> $2::uuid`,
		tenantID, id); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `
		UPDATE vector_store_configs SET is_primary = TRUE, updated_at = now()
		 WHERE tenant_id = $1 AND id = $2::uuid`,
		tenantID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return vectorstore.ErrNotFound
	}
	return tx.Commit(ctx)
}

func (s *VectorStoreConfigStore) Primary(ctx context.Context, tenantID string) (*vectorstore.Config, error) {
	const q = `
		SELECT id::text, tenant_id, kind, name, endpoint, index_name,
		       class_name, collection, dimension, secret_urn, is_primary,
		       COALESCE(last_tested_at, '0001-01-01 00:00:00+00'::timestamptz),
		       last_test_ok, last_test_error, created_at, updated_at
		  FROM vector_store_configs
		 WHERE tenant_id = $1 AND is_primary
		 LIMIT 1`
	c, err := scanVectorStore(s.pool.QueryRow(ctx, q, tenantID))
	if errors.Is(err, vectorstore.ErrNotFound) {
		return nil, vectorstore.ErrNotFound
	}
	return c, err
}

func (s *VectorStoreConfigStore) SetSecretURN(ctx context.Context, tenantID, id, urn string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE vector_store_configs SET secret_urn = $3, updated_at = now()
		 WHERE tenant_id = $1 AND id = $2::uuid`,
		tenantID, id, urn)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return vectorstore.ErrNotFound
	}
	return nil
}

func scanVectorStore(row rowScanner) (*vectorstore.Config, error) {
	var (
		c    vectorstore.Config
		kind string
	)
	if err := row.Scan(
		&c.ID, &c.TenantID, &kind, &c.Name, &c.Endpoint, &c.IndexName,
		&c.ClassName, &c.Collection, &c.Dimension, &c.SecretURN, &c.IsPrimary,
		&c.LastTestedAt, &c.LastTestOK, &c.LastTestErr,
		&c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, vectorstore.ErrNotFound
		}
		return nil, fmt.Errorf("scan vector_store_config: %w", err)
	}
	c.Kind = vectorstore.Kind(kind)
	return &c, nil
}

var _ vectorstore.Repo = (*VectorStoreConfigStore)(nil)
