package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/secrets"
)

// SecretsStore satisfies secrets.Storage against the `secrets` table
// (migration 0006). The vault layer above does the AEAD; this file only
// shuttles ciphertext + nonce in and out.
type SecretsStore struct {
	pool *pgxpool.Pool
}

func NewSecretsStore(pool *pgxpool.Pool) *SecretsStore {
	return &SecretsStore{pool: pool}
}

func (s *SecretsStore) PutSealed(ctx context.Context, tenantID, urn string, sealed secrets.Sealed) error {
	const q = `
		INSERT INTO secrets (urn, tenant_id, nonce, ciphertext, updated_at)
		VALUES ($1, $2::uuid, $3, $4, NOW())
		ON CONFLICT (urn) DO UPDATE
		   SET nonce      = EXCLUDED.nonce,
		       ciphertext = EXCLUDED.ciphertext,
		       updated_at = NOW()
		 WHERE secrets.tenant_id = EXCLUDED.tenant_id`
	tag, err := s.pool.Exec(ctx, q, urn, tenantID, sealed.Nonce, sealed.Ciphertext)
	if err != nil {
		return fmt.Errorf("secrets: put: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errors.New("secrets: tenant mismatch on existing urn")
	}
	return nil
}

func (s *SecretsStore) GetSealed(ctx context.Context, tenantID, urn string) (secrets.Sealed, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT nonce, ciphertext, updated_at
		  FROM secrets
		 WHERE urn = $1 AND tenant_id = $2::uuid`, urn, tenantID)
	var out secrets.Sealed
	if err := row.Scan(&out.Nonce, &out.Ciphertext, &out.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return secrets.Sealed{}, secrets.ErrNotFound
		}
		return secrets.Sealed{}, fmt.Errorf("secrets: get: %w", err)
	}
	return out, nil
}

func (s *SecretsStore) DeleteSealed(ctx context.Context, tenantID, urn string) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM secrets WHERE urn = $1 AND tenant_id = $2::uuid`,
		urn, tenantID)
	if err != nil {
		return fmt.Errorf("secrets: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return secrets.ErrNotFound
	}
	return nil
}
