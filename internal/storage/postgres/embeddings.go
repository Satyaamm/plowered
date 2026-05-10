package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/search"
)

// EmbeddingStore is the Postgres-backed search.Store. Vectors live in
// asset_embeddings.embedding as JSONB float arrays — pgvector is a
// drop-in replacement when catalogs grow past the in-Go cosine scan's
// comfort zone (~10k assets per tenant).
type EmbeddingStore struct {
	pool *pgxpool.Pool
}

func NewEmbeddingStore(p *pgxpool.Pool) *EmbeddingStore { return &EmbeddingStore{pool: p} }

func (s *EmbeddingStore) Upsert(ctx context.Context, tenantID, assetID, model string, vec []float32) error {
	body, err := json.Marshal(vec)
	if err != nil {
		return fmt.Errorf("marshal vector: %w", err)
	}
	const q = `
		INSERT INTO asset_embeddings (asset_id, tenant_id, model, dim, embedding)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5)
		ON CONFLICT (asset_id, model)
		DO UPDATE SET tenant_id = EXCLUDED.tenant_id,
		              dim = EXCLUDED.dim,
		              embedding = EXCLUDED.embedding,
		              updated_at = now()`
	if _, err := s.pool.Exec(ctx, q, assetID, tenantID, model, len(vec), body); err != nil {
		return fmt.Errorf("upsert embedding: %w", err)
	}
	return nil
}

func (s *EmbeddingStore) List(ctx context.Context, tenantID, model string) ([]search.Row, error) {
	const q = `
		SELECT asset_id::text, embedding
		  FROM asset_embeddings
		 WHERE tenant_id = $1::uuid AND model = $2`
	rows, err := s.pool.Query(ctx, q, tenantID, model)
	if err != nil {
		return nil, fmt.Errorf("list embeddings: %w", err)
	}
	defer rows.Close()
	var out []search.Row
	for rows.Next() {
		var (
			id  string
			raw []byte
		)
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var vec []float32
		if err := json.Unmarshal(raw, &vec); err != nil {
			return nil, fmt.Errorf("decode embedding %s: %w", id, err)
		}
		out = append(out, search.Row{AssetID: id, Vector: vec})
	}
	return out, rows.Err()
}

func (s *EmbeddingStore) DeleteForAsset(ctx context.Context, assetID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM asset_embeddings WHERE asset_id = $1::uuid`, assetID)
	return err
}

var _ search.Store = (*EmbeddingStore)(nil)
