package vectorstore

import (
	"context"
	"errors"
)

// AssetEmbeddingsStore is the contract the pgvector backend needs
// against the existing asset_embeddings table. Implemented by the
// storage/postgres layer; here we accept the interface so this package
// stays driver-free.
type AssetEmbeddingsStore interface {
	UpsertEmbedding(ctx context.Context, tenantID, assetID string, vec []float32, metadata map[string]string) error
	SearchEmbeddings(ctx context.Context, tenantID string, query []float32, k int, filter map[string]string) ([]SearchHit, error)
	DeleteEmbeddings(ctx context.Context, tenantID string, ids []string) error
}

// pgvectorStore wraps the existing asset_embeddings table. The semantic
// search code already writes there; this adapter just hands the
// resolver a Store the rest of the platform can call uniformly.
type pgvectorStore struct {
	cfg   *Config
	store AssetEmbeddingsStore
}

// NewPgvectorStore returns a Store backed by the supplied
// AssetEmbeddingsStore. Wired in cmd/plowered/main.go where the pgx
// pool exists; this keeps the pgx import out of the core package.
func NewPgvectorStore(cfg *Config, store AssetEmbeddingsStore) Store {
	return &pgvectorStore{cfg: cfg, store: store}
}

func (p *pgvectorStore) Name() string { return "pgvector:asset_embeddings" }

func (p *pgvectorStore) Upsert(ctx context.Context, vecs []Vector) error {
	if p.store == nil {
		return errors.New("pgvector: asset_embeddings store not wired")
	}
	for _, v := range vecs {
		if err := p.store.UpsertEmbedding(ctx, p.cfg.TenantID, v.ID, v.Values, v.Metadata); err != nil {
			return err
		}
	}
	return nil
}

func (p *pgvectorStore) Search(ctx context.Context, opts SearchOpts) ([]SearchHit, error) {
	if p.store == nil {
		return nil, errors.New("pgvector: asset_embeddings store not wired")
	}
	tenant := opts.TenantID
	if tenant == "" {
		tenant = p.cfg.TenantID
	}
	return p.store.SearchEmbeddings(ctx, tenant, opts.Vector, opts.K, opts.Filter)
}

func (p *pgvectorStore) Delete(ctx context.Context, ids []string) error {
	if p.store == nil {
		return errors.New("pgvector: asset_embeddings store not wired")
	}
	return p.store.DeleteEmbeddings(ctx, p.cfg.TenantID, ids)
}

// activePgvectorStore is the registered AssetEmbeddingsStore. Wired
// once at process boot via SetPgvectorStore from a package that has
// the pgxpool.
var activePgvectorStore AssetEmbeddingsStore

// SetPgvectorStore registers the asset_embeddings backend. Memory mode
// + tests can leave this nil; KindPgvector then returns a helpful
// error from Build instead of silently misbehaving.
func SetPgvectorStore(s AssetEmbeddingsStore) { activePgvectorStore = s }
