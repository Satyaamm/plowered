package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/vectorstore"
)

// PgvectorAssetEmbeddings adapts the existing asset_embeddings table to
// the vectorstore.AssetEmbeddingsStore interface. The asset_embeddings
// schema stores vectors as JSONB float arrays, so Search is an
// in-process cosine scan — fine up to ~10k vectors per tenant; for
// larger workloads point at Pinecone / Weaviate / Qdrant via the
// vectorstore.Config.
//
// model + dim are pinned to the platform-default values here. When the
// resolver layer grows multi-model embeddings, this adapter widens to
// take model from the request.
type PgvectorAssetEmbeddings struct {
	pool  *pgxpool.Pool
	model string
}

// NewPgvectorAssetEmbeddings returns an adapter bound to the supplied
// pool + model id. Call SetPgvectorStore with the result during
// process boot.
func NewPgvectorAssetEmbeddings(p *pgxpool.Pool, model string) *PgvectorAssetEmbeddings {
	if model == "" {
		model = "default"
	}
	return &PgvectorAssetEmbeddings{pool: p, model: model}
}

func (s *PgvectorAssetEmbeddings) UpsertEmbedding(ctx context.Context, tenantID, assetID string, vec []float32, _ map[string]string) error {
	body, err := json.Marshal(vec)
	if err != nil {
		return fmt.Errorf("marshal vector: %w", err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO asset_embeddings (asset_id, tenant_id, model, dim, embedding)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5)
		ON CONFLICT (asset_id, model)
		DO UPDATE SET tenant_id = EXCLUDED.tenant_id,
		              dim = EXCLUDED.dim,
		              embedding = EXCLUDED.embedding,
		              updated_at = now()`,
		assetID, tenantID, s.model, len(vec), body)
	if err != nil {
		return fmt.Errorf("upsert embedding: %w", err)
	}
	return nil
}

func (s *PgvectorAssetEmbeddings) SearchEmbeddings(ctx context.Context, tenantID string, query []float32, k int, _ map[string]string) ([]vectorstore.SearchHit, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT asset_id::text, embedding
		  FROM asset_embeddings
		 WHERE tenant_id = $1::uuid AND model = $2`,
		tenantID, s.model)
	if err != nil {
		return nil, fmt.Errorf("search embeddings: %w", err)
	}
	defer rows.Close()
	hits := []vectorstore.SearchHit{}
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
			continue // skip malformed row rather than fail the whole query
		}
		hits = append(hits, vectorstore.SearchHit{ID: id, Score: cosine(query, vec)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if k <= 0 {
		k = 10
	}
	if len(hits) > k {
		hits = hits[:k]
	}
	return hits, nil
}

func (s *PgvectorAssetEmbeddings) DeleteEmbeddings(ctx context.Context, _ string, ids []string) error {
	for _, id := range ids {
		if _, err := s.pool.Exec(ctx,
			`DELETE FROM asset_embeddings WHERE asset_id = $1::uuid`, id,
		); err != nil {
			return err
		}
	}
	return nil
}

func cosine(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		ai, bi := float64(a[i]), float64(b[i])
		dot += ai * bi
		na += ai * ai
		nb += bi * bi
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}

var _ vectorstore.AssetEmbeddingsStore = (*PgvectorAssetEmbeddings)(nil)
