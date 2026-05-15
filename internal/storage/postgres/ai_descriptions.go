package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/describer"
	"github.com/Satyaamm/plowered/internal/core/graph"
	"github.com/Satyaamm/plowered/internal/storage"
)

// AIDescriptionStore is the Postgres implementation of
// describer.Log AND aictx.AssetReader. Bundling keeps cmd-wiring
// small — main.go constructs one struct and passes it to both
// services that need it.
type AIDescriptionStore struct {
	pool  *pgxpool.Pool
	store *Store // for asset reads — reuses GetAsset's tenant-from-ctx logic
}

func NewAIDescriptionStore(pool *pgxpool.Pool, store *Store) *AIDescriptionStore {
	return &AIDescriptionStore{pool: pool, store: store}
}

// ----- describer.Log -------------------------------------------------

func (s *AIDescriptionStore) Record(ctx context.Context, sug *describer.Suggestion, tenantID, generatedBy string) error {
	if sug == nil || sug.AssetID == "" {
		return errors.New("describer log: missing asset_id")
	}
	const q = `
		INSERT INTO ai_descriptions_log
			(tenant_id, asset_id, model, suggestion,
			 input_tokens, output_tokens, generated_at, generated_by)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, NULLIF($8,'')::uuid)`
	_, err := s.pool.Exec(ctx, q,
		tenantID, sug.AssetID, sug.Model, sug.Suggestion,
		sug.InputTokens, sug.OutputTokens, sug.GeneratedAt, generatedBy,
	)
	if err != nil {
		return fmt.Errorf("write ai_descriptions_log: %w", err)
	}
	return nil
}

func (s *AIDescriptionStore) MarkAccepted(ctx context.Context, tenantID, suggestionID string) error {
	const q = `
		UPDATE ai_descriptions_log
		   SET accepted = true, accepted_at = now()
		 WHERE tenant_id = $1::uuid AND id = $2::uuid`
	_, err := s.pool.Exec(ctx, q, tenantID, suggestionID)
	return err
}

// ----- aictx.AssetReader ---------------------------------------------

// GetAsset adapts storage.Store.GetAsset (which reads tenant from ctx)
// to the aictx.AssetReader contract (tenant explicit). The adapter is
// a tiny tenant-into-ctx hop.
func (s *AIDescriptionStore) GetAsset(ctx context.Context, tenantID, assetID string) (*graph.Asset, error) {
	if s.store == nil {
		return nil, errors.New("describer: storage.Store not wired")
	}
	return s.store.GetAsset(storage.WithTenant(ctx, tenantID), assetID)
}
