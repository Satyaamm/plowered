// Package search implements vector-similarity search over the asset
// catalog. Embeddings live in public.asset_embeddings; the local
// provider in pkg/llm/local makes the pipeline runnable offline. A real
// LLM provider is a one-line swap in the cmd/ wiring.
//
// Two surfaces:
//
//	Indexer.IndexAll  — walks the catalog, embeds each asset's
//	                    qualified_name + description + tags, upserts.
//	Searcher.Query    — embeds a free-text query, scans embeddings for
//	                    the tenant, returns the top-K cosine matches.
//
// Search is policy-aware: a Resolver can be plugged in to filter
// results through the policy engine before they reach the caller. The
// MCP server already filters on the call path; the HTTP /v1/search
// endpoint relies on this resolver instead.
package search

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Satyaamm/plowered/internal/core/graph"
	"github.com/Satyaamm/plowered/internal/storage"
	"github.com/Satyaamm/plowered/pkg/llm"
	"github.com/Satyaamm/plowered/pkg/llm/local"
)

// Store is the persistence interface the indexer + searcher need. The
// Postgres implementation lives in internal/storage/postgres.
type Store interface {
	Upsert(ctx context.Context, tenantID, assetID, model string, vec []float32) error
	List(ctx context.Context, tenantID, model string) ([]Row, error)
	DeleteForAsset(ctx context.Context, assetID string) error
}

// Row is one (asset_id, vector) pair returned by the store. The
// orchestrator joins this with assets at query time so callers receive
// fully populated hits.
type Row struct {
	AssetID string
	Vector  []float32
}

// Filter optionally drops a hit before it's returned to the caller. A
// "deny PII to viewers" rule lives upstream in the policy engine; the
// HTTP handler wires the engine into a Filter so search inherits the
// same access decisions as direct catalog reads.
type Filter func(ctx context.Context, asset *graph.Asset) bool

// Hit is one ranked search result with its similarity score.
type Hit struct {
	Asset *graph.Asset
	Score float32
}

// Indexer walks the catalog and writes embeddings.
type Indexer struct {
	Catalog  storage.Store
	Provider llm.Provider
	Store    Store
}

// IndexAsset embeds a single asset and upserts it. Used by the catalog
// crawler when it lands a new asset (or could be — wiring is a follow-
// up). Safe to call repeatedly; the upsert dedupes on (asset_id, model).
func (ix *Indexer) IndexAsset(ctx context.Context, a *graph.Asset) error {
	if ix.Provider == nil {
		return errors.New("search: indexer provider not configured")
	}
	resp, err := ix.Provider.Embed(ctx, llm.EmbedRequest{
		Model: ix.modelName(),
		Texts: []string{textForAsset(a)},
	})
	if err != nil {
		return fmt.Errorf("embed: %w", err)
	}
	if len(resp.Vectors) == 0 {
		return errors.New("embed: empty response")
	}
	return ix.Store.Upsert(ctx, a.TenantID, a.ID, ix.modelName(), resp.Vectors[0])
}

// IndexAll lists every asset for the tenant in pages, embedding in
// batches of 32 (a reasonable batch size for any provider). Returns the
// number of rows written and the first error encountered.
func (ix *Indexer) IndexAll(ctx context.Context, tenantID string) (int, error) {
	if ix.Provider == nil {
		return 0, errors.New("search: indexer provider not configured")
	}
	model := ix.modelName()
	written := 0
	page := ""
	for {
		assets, next, err := ix.Catalog.ListAssets(ctx, storage.ListAssetsOptions{
			PageSize: 200, PageToken: page,
		})
		if err != nil {
			return written, fmt.Errorf("list assets: %w", err)
		}
		batchSize := 32
		for i := 0; i < len(assets); i += batchSize {
			end := i + batchSize
			if end > len(assets) {
				end = len(assets)
			}
			batch := assets[i:end]
			texts := make([]string, len(batch))
			for j, a := range batch {
				texts[j] = textForAsset(a)
			}
			resp, err := ix.Provider.Embed(ctx, llm.EmbedRequest{Model: model, Texts: texts})
			if err != nil {
				return written, fmt.Errorf("embed batch: %w", err)
			}
			for j, a := range batch {
				if j >= len(resp.Vectors) {
					continue
				}
				if err := ix.Store.Upsert(ctx, tenantID, a.ID, model, resp.Vectors[j]); err != nil {
					return written, fmt.Errorf("upsert %s: %w", a.ID, err)
				}
				written++
			}
		}
		if next == "" {
			break
		}
		page = next
	}
	return written, nil
}

func (ix *Indexer) modelName() string {
	if ix.Provider == nil {
		return local.Name
	}
	return ix.Provider.Name()
}

// Searcher scores a query against stored embeddings.
type Searcher struct {
	Catalog  storage.Store
	Provider llm.Provider
	Store    Store
	Filter   Filter // optional
}

// Query embeds q, fetches every embedding for the tenant, and returns
// the top k hits by cosine similarity. The query model must match the
// model that produced the index; we default to the provider's name.
func (s *Searcher) Query(ctx context.Context, tenantID, q string, k int) ([]Hit, error) {
	if strings.TrimSpace(q) == "" {
		return nil, errors.New("search: query is required")
	}
	if k <= 0 || k > 100 {
		k = 20
	}
	model := s.modelName()
	resp, err := s.Provider.Embed(ctx, llm.EmbedRequest{Model: model, Texts: []string{q}})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(resp.Vectors) == 0 {
		return nil, errors.New("embed query: empty response")
	}
	qv := resp.Vectors[0]

	rows, err := s.Store.List(ctx, tenantID, model)
	if err != nil {
		return nil, fmt.Errorf("list embeddings: %w", err)
	}
	type scored struct {
		AssetID string
		Score   float32
	}
	scoreds := make([]scored, 0, len(rows))
	for _, r := range rows {
		score := local.Cosine(qv, r.Vector)
		if score <= 0 {
			continue
		}
		scoreds = append(scoreds, scored{AssetID: r.AssetID, Score: score})
	}
	sort.Slice(scoreds, func(i, j int) bool { return scoreds[i].Score > scoreds[j].Score })
	// Keep enough headroom to backfill if the policy filter drops some.
	want := k * 3
	if want > len(scoreds) {
		want = len(scoreds)
	}
	scoreds = scoreds[:want]

	hits := make([]Hit, 0, k)
	for _, sc := range scoreds {
		a, err := s.Catalog.GetAsset(ctx, sc.AssetID)
		if err != nil {
			continue
		}
		if s.Filter != nil && !s.Filter(ctx, a) {
			continue
		}
		hits = append(hits, Hit{Asset: a, Score: sc.Score})
		if len(hits) >= k {
			break
		}
	}
	return hits, nil
}

func (s *Searcher) modelName() string {
	if s.Provider == nil {
		return local.Name
	}
	return s.Provider.Name()
}

// textForAsset is the input we hand to the embedder. We bias toward
// descriptive fields (qualified name, description, tags) over things
// the user would never type (UUIDs, timestamps).
func textForAsset(a *graph.Asset) string {
	parts := []string{a.QualifiedName, a.Name, string(a.Type), a.Description}
	if len(a.Tags) > 0 {
		parts = append(parts, strings.Join(a.Tags, " "))
	}
	if len(a.Owners) > 0 {
		parts = append(parts, strings.Join(a.Owners, " "))
	}
	return strings.Join(parts, " ")
}
