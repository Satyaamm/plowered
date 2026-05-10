// Package crawler walks a customer datasource and projects what it
// finds (schemas, tables, columns) into the Plowered catalog as Assets +
// Edges + Tags. The actual driver-level reads live in
// internal/adapters/<source>/; this package owns the upsert semantics +
// PII tagging that everyone shares.
//
// Idempotency: the assets table has UNIQUE(tenant_id, qualified_name).
// Re-crawling the same source does NOT create duplicates — existing
// rows are updated in place, missing rows are created, and rows that no
// longer exist in the source are left untouched (a "deleted_columns"
// reconciliation lands in step 2 alongside lineage; for v0 a follow-up
// crawl is the source of truth for what's there now, not what's been
// removed).
package crawler

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Satyaamm/plowered/internal/core/graph"
	"github.com/Satyaamm/plowered/internal/storage"
)

// Tree is what the source adapter returns: every schema, every table,
// every column, in one shot. Big warehouses will eventually need a
// streaming sink to avoid buffering 100K+ rows; for v0 the catalog sizes
// we care about all fit comfortably in memory.
type Tree struct {
	Schemas []SchemaInfo
}

type SchemaInfo struct {
	Name   string
	Tables []TableInfo
}

type TableInfo struct {
	Name        string
	Kind        string // "table" or "view"
	Description string
	Columns     []ColumnInfo
}

type ColumnInfo struct {
	Name        string
	DataType    string
	Nullable    bool
	OrdinalPos  int
	Default     string
	Description string
}

// Source is what concrete adapters implement. Each source crawls its own
// information_schema (or equivalent) and returns the Tree.
type Source interface {
	Crawl(ctx context.Context, cfg map[string]any, secret []byte) (*Tree, error)
}

// Result summarises a crawl for the API response.
type Result struct {
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at"`
	SchemaCount  int       `json:"schemas"`
	TableCount   int       `json:"tables"`
	ColumnCount  int       `json:"columns"`
	TaggedCount  int       `json:"tagged_columns"`
	UpdatedCount int       `json:"updated"`
	CreatedCount int       `json:"created"`
}

// Crawler is the orchestrator: walk Tree + upsert into storage.Store.
type Crawler struct {
	Store      storage.Store
	Classifier ColumnNameClassifier
	Logger     *slog.Logger
	Now        func() time.Time
}

// New returns a Crawler with sensible defaults. Pass nil for Logger to
// inherit slog.Default().
func New(store storage.Store, logger *slog.Logger) *Crawler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Crawler{
		Store:      store,
		Classifier: NewColumnNameClassifier(),
		Logger:     logger,
		Now:        time.Now,
	}
}

// Run projects the tree onto the catalog. The connection's name is the
// top of the qualified-name namespace so two connections with the same
// schema names don't collide.
func (c *Crawler) Run(ctx context.Context, connectionName string, tree *Tree, actor string) (*Result, error) {
	if c.Store == nil {
		return nil, fmt.Errorf("crawler: storage.Store is required")
	}
	if tree == nil {
		return nil, fmt.Errorf("crawler: nil tree")
	}
	r := &Result{StartedAt: c.now().UTC()}

	for _, sch := range tree.Schemas {
		r.SchemaCount++
		schemaQN := strings.Join([]string{connectionName, sch.Name}, ".")
		schemaAsset, created, err := c.upsertAsset(ctx, &graph.Asset{
			QualifiedName: schemaQN,
			Type:          graph.AssetTypeSchema,
			Name:          sch.Name,
			Properties: map[string]any{
				"connection": connectionName,
			},
			CreatedBy: actor,
			UpdatedBy: actor,
		})
		if err != nil {
			return r, fmt.Errorf("upsert schema %s: %w", schemaQN, err)
		}
		c.bump(r, created)

		for _, t := range sch.Tables {
			r.TableCount++
			tableQN := schemaQN + "." + t.Name
			assetType := graph.AssetTypeTable
			if t.Kind == "view" {
				assetType = graph.AssetTypeView
			}
			tableAsset, created, err := c.upsertAsset(ctx, &graph.Asset{
				QualifiedName: tableQN,
				Type:          assetType,
				Name:          t.Name,
				Description:   t.Description,
				Properties: map[string]any{
					"connection": connectionName,
					"schema":     sch.Name,
					"kind":       t.Kind,
				},
				CreatedBy: actor,
				UpdatedBy: actor,
			})
			if err != nil {
				return r, fmt.Errorf("upsert table %s: %w", tableQN, err)
			}
			c.bump(r, created)

			// schema --[contains]--> table
			if err := c.upsertEdge(ctx, schemaAsset.ID, tableAsset.ID, graph.EdgeDefines); err != nil {
				return r, fmt.Errorf("edge schema→table %s: %w", tableQN, err)
			}

			for _, col := range t.Columns {
				r.ColumnCount++
				colQN := tableQN + "." + col.Name
				tags := c.Classifier.Classify(col.Name)
				if len(tags) > 0 {
					r.TaggedCount++
				}
				colAsset, created, err := c.upsertAsset(ctx, &graph.Asset{
					QualifiedName: colQN,
					Type:          graph.AssetTypeColumn,
					Name:          col.Name,
					Description:   col.Description,
					Tags:          tags,
					Properties: map[string]any{
						"connection":   connectionName,
						"schema":       sch.Name,
						"table":        t.Name,
						"data_type":    col.DataType,
						"nullable":     col.Nullable,
						"ordinal_pos":  col.OrdinalPos,
						"default":      col.Default,
					},
					CreatedBy: actor,
					UpdatedBy: actor,
				})
				if err != nil {
					return r, fmt.Errorf("upsert column %s: %w", colQN, err)
				}
				c.bump(r, created)
				if err := c.upsertEdge(ctx, tableAsset.ID, colAsset.ID, graph.EdgeDefines); err != nil {
					return r, fmt.Errorf("edge table→col %s: %w", colQN, err)
				}
			}
		}
	}

	r.FinishedAt = c.now().UTC()
	c.Logger.Info("crawler: done",
		"connection", connectionName,
		"schemas", r.SchemaCount,
		"tables", r.TableCount,
		"columns", r.ColumnCount,
		"tagged", r.TaggedCount,
		"created", r.CreatedCount,
		"updated", r.UpdatedCount,
		"duration_ms", r.FinishedAt.Sub(r.StartedAt).Milliseconds(),
	)
	return r, nil
}

// upsertAsset finds-or-creates by qualified_name. Existing rows are
// updated in place so re-crawls preserve hand-edits to anything we don't
// overwrite (owners, AI descriptions, certifications).
func (c *Crawler) upsertAsset(ctx context.Context, a *graph.Asset) (*graph.Asset, bool, error) {
	existing, err := c.Store.GetAssetByQualifiedName(ctx, a.QualifiedName)
	if err == nil && existing != nil {
		// Preserve fields a human or downstream worker may have set.
		merged := *existing
		merged.Description = pickNonEmpty(a.Description, existing.Description)
		merged.Properties = mergeProps(existing.Properties, a.Properties)
		// Tags: union new (auto-classified) with existing (manual + auto)
		merged.Tags = unionTags(existing.Tags, a.Tags)
		merged.UpdatedBy = a.UpdatedBy
		out, err := c.Store.UpdateAsset(ctx, &merged)
		if err != nil {
			return nil, false, err
		}
		return out, false, nil
	}
	out, err := c.Store.CreateAsset(ctx, a)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// upsertEdge is silent on duplicates because the edges table has a
// UNIQUE(tenant_id, kind, source_id, target_id) constraint and the
// Postgres store surfaces that as ErrConflict. The crawler treats it as
// "already there" and moves on.
func (c *Crawler) upsertEdge(ctx context.Context, sourceID, targetID string, kind graph.EdgeKind) error {
	_, err := c.Store.CreateEdge(ctx, &graph.Edge{
		Kind:     kind,
		SourceID: sourceID,
		TargetID: targetID,
	})
	if err == nil {
		return nil
	}
	if errIsConflict(err) {
		return nil
	}
	return err
}

func errIsConflict(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "conflict") || strings.Contains(err.Error(), "duplicate")
}

func (c *Crawler) bump(r *Result, created bool) {
	if created {
		r.CreatedCount++
		return
	}
	r.UpdatedCount++
}

func (c *Crawler) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func pickNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func mergeProps(existing, fresh map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range existing {
		out[k] = v
	}
	for k, v := range fresh {
		out[k] = v
	}
	return out
}

func unionTags(a, b []string) []string {
	seen := map[string]struct{}{}
	for _, x := range a {
		seen[x] = struct{}{}
	}
	for _, x := range b {
		seen[x] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}
