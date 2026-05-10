package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/aiprovider"
)

// AIProviderStore is the Postgres-backed Repo for aiprovider.Config rows.
type AIProviderStore struct {
	pool *pgxpool.Pool
}

func NewAIProviderStore(p *pgxpool.Pool) *AIProviderStore { return &AIProviderStore{pool: p} }

func (s *AIProviderStore) Create(ctx context.Context, c *aiprovider.Config) (*aiprovider.Config, error) {
	const q = `
		INSERT INTO ai_provider_configs
		    (tenant_id, kind, name, model, base_url, secret_urn, capability, is_primary)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id::text, created_at, updated_at`
	if err := s.pool.QueryRow(ctx, q,
		c.TenantID, string(c.Kind), c.Name, c.Model, c.BaseURL, c.SecretURN, string(c.Capability), c.IsPrimary,
	).Scan(&c.ID, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, fmt.Errorf("create ai_provider_config: %w", err)
	}
	return c, nil
}

func (s *AIProviderStore) Get(ctx context.Context, tenantID, id string) (*aiprovider.Config, error) {
	const q = `
		SELECT id::text, tenant_id, kind, name, model, base_url, secret_urn,
		       capability, is_primary,
		       COALESCE(last_tested_at, '0001-01-01 00:00:00+00'::timestamptz),
		       last_test_ok, last_test_error, created_at, updated_at
		  FROM ai_provider_configs
		 WHERE tenant_id = $1 AND id = $2::uuid`
	row := s.pool.QueryRow(ctx, q, tenantID, id)
	return scanAIProvider(row)
}

func (s *AIProviderStore) List(ctx context.Context, tenantID string) ([]*aiprovider.Config, error) {
	const q = `
		SELECT id::text, tenant_id, kind, name, model, base_url, secret_urn,
		       capability, is_primary,
		       COALESCE(last_tested_at, '0001-01-01 00:00:00+00'::timestamptz),
		       last_test_ok, last_test_error, created_at, updated_at
		  FROM ai_provider_configs
		 WHERE tenant_id = $1
		 ORDER BY created_at DESC`
	rows, err := s.pool.Query(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list ai_provider_configs: %w", err)
	}
	defer rows.Close()
	var out []*aiprovider.Config
	for rows.Next() {
		c, err := scanAIProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *AIProviderStore) Update(ctx context.Context, c *aiprovider.Config) (*aiprovider.Config, error) {
	const q = `
		UPDATE ai_provider_configs
		   SET name = $3, model = $4, base_url = $5, capability = $6,
		       updated_at = now()
		 WHERE tenant_id = $1 AND id = $2::uuid
		RETURNING updated_at`
	if err := s.pool.QueryRow(ctx, q,
		c.TenantID, c.ID, c.Name, c.Model, c.BaseURL, string(c.Capability),
	).Scan(&c.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, aiprovider.ErrNotFound
		}
		return nil, fmt.Errorf("update ai_provider_config: %w", err)
	}
	return c, nil
}

func (s *AIProviderStore) Delete(ctx context.Context, tenantID, id string) error {
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM ai_provider_configs WHERE tenant_id = $1 AND id = $2::uuid`,
		tenantID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return aiprovider.ErrNotFound
	}
	return nil
}

// SetSecretURN writes the vault URN onto an existing config row.
// Called once after Create returns the generated UUID — the URN can't
// be known earlier because it incorporates the ID.
func (s *AIProviderStore) SetSecretURN(ctx context.Context, tenantID, id, urn string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE ai_provider_configs SET secret_urn = $3, updated_at = now()
		 WHERE tenant_id = $1 AND id = $2::uuid`,
		tenantID, id, urn)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return aiprovider.ErrNotFound
	}
	return nil
}

func (s *AIProviderStore) MarkTested(ctx context.Context, tenantID, id string, ok bool, errMsg string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE ai_provider_configs
		   SET last_tested_at = now(),
		       last_test_ok = $3,
		       last_test_error = $4
		 WHERE tenant_id = $1 AND id = $2::uuid`,
		tenantID, id, ok, errMsg)
	return err
}

// SetPrimary atomically marks one config primary for its capability and
// demotes any sibling primary in the same (tenant, capability) bucket.
// A unique partial index enforces the invariant at the DB level — this
// function is the cooperating writer.
func (s *AIProviderStore) SetPrimary(ctx context.Context, tenantID, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var capability string
	if err := tx.QueryRow(ctx,
		`SELECT capability FROM ai_provider_configs WHERE tenant_id = $1 AND id = $2::uuid`,
		tenantID, id,
	).Scan(&capability); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return aiprovider.ErrNotFound
		}
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE ai_provider_configs SET is_primary = FALSE
		 WHERE tenant_id = $1 AND capability = $2 AND id <> $3::uuid`,
		tenantID, capability, id); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE ai_provider_configs SET is_primary = TRUE, updated_at = now()
		 WHERE tenant_id = $1 AND id = $2::uuid`,
		tenantID, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func scanAIProvider(row rowScanner) (*aiprovider.Config, error) {
	var (
		c          aiprovider.Config
		kind       string
		capability string
	)
	if err := row.Scan(
		&c.ID, &c.TenantID, &kind, &c.Name, &c.Model, &c.BaseURL, &c.SecretURN,
		&capability, &c.IsPrimary,
		&c.LastTestedAt, &c.LastTestOK, &c.LastTestErr,
		&c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, aiprovider.ErrNotFound
		}
		return nil, fmt.Errorf("scan ai_provider_config: %w", err)
	}
	c.Kind = aiprovider.Kind(kind)
	c.Capability = aiprovider.Capability(capability)
	return &c, nil
}

var _ aiprovider.Repo = (*AIProviderStore)(nil)
