package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/classifier"
)

// ClassifyCatalog implements classifier.CatalogReader and the small
// MergeAssetTags surface the orchestrator's sink needs. Both reads and
// writes live here so the cmd/ wiring only constructs one struct.
type ClassifyCatalog struct {
	pool *pgxpool.Pool
}

func NewClassifyCatalog(p *pgxpool.Pool) *ClassifyCatalog {
	return &ClassifyCatalog{pool: p}
}

// TablesForConnection returns every table-asset whose qualified_name
// starts with the connection's name. The classifier needs (schema,
// table) pairs that map to live database tables; properties.connection
// is the canonical way to filter, with a fallback to the qn prefix.
func (c *ClassifyCatalog) TablesForConnection(ctx context.Context, tenantID, connectionID string) ([]classifier.TableRef, error) {
	const q = `
		SELECT a.id::text, COALESCE(a.properties->>'schema',''), a.name
		  FROM assets a
		  JOIN connections c ON c.tenant_id::text = a.tenant_id
		 WHERE a.tenant_id = $1
		   AND c.id = $2::uuid
		   AND a.type = 'table'
		   AND a.qualified_name LIKE c.name || '.%'`
	rows, err := c.pool.Query(ctx, q, tenantID, connectionID)
	if err != nil {
		return nil, fmt.Errorf("tables for connection: %w", err)
	}
	defer rows.Close()
	var out []classifier.TableRef
	for rows.Next() {
		var ref classifier.TableRef
		if err := rows.Scan(&ref.AssetID, &ref.Schema, &ref.Name); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

// ColumnsForTable lists column-asset rows whose qualified_name has the
// table's qn as the parent prefix.
func (c *ClassifyCatalog) ColumnsForTable(ctx context.Context, tenantID, tableAssetID string) ([]classifier.ColumnRef, error) {
	const q = `
		WITH t AS (
			SELECT qualified_name FROM assets
			 WHERE tenant_id = $1 AND id = $2::uuid
		)
		SELECT a.id::text, a.name, a.qualified_name
		  FROM assets a, t
		 WHERE a.tenant_id = $1
		   AND a.type = 'column'
		   AND a.qualified_name LIKE t.qualified_name || '.%'`
	rows, err := c.pool.Query(ctx, q, tenantID, tableAssetID)
	if err != nil {
		return nil, fmt.Errorf("columns for table: %w", err)
	}
	defer rows.Close()
	var out []classifier.ColumnRef
	for rows.Next() {
		var ref classifier.ColumnRef
		if err := rows.Scan(&ref.AssetID, &ref.Name, &ref.QualifiedName); err != nil {
			return nil, err
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

// MergeAssetTags unions the supplied tags into assets.tags. Existing
// tags are preserved so manual classifications + name-based heuristics
// + sampled detections stack additively.
func (c *ClassifyCatalog) MergeAssetTags(ctx context.Context, tenantID, assetID string, tags []string) error {
	if len(tags) == 0 {
		return nil
	}
	const q = `SELECT tags FROM assets WHERE tenant_id = $1 AND id = $2::uuid`
	var existing []byte
	if err := c.pool.QueryRow(ctx, q, tenantID, assetID).Scan(&existing); err != nil {
		return fmt.Errorf("read existing tags: %w", err)
	}
	var prev []string
	if len(existing) > 0 {
		_ = json.Unmarshal(existing, &prev)
	}
	seen := map[string]struct{}{}
	merged := make([]string, 0, len(prev)+len(tags))
	for _, t := range prev {
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		merged = append(merged, t)
	}
	for _, t := range tags {
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		merged = append(merged, t)
	}
	mergedJSON, _ := json.Marshal(merged)
	const upd = `UPDATE assets SET tags = $3, updated_at = now() WHERE tenant_id = $1 AND id = $2::uuid`
	if _, err := c.pool.Exec(ctx, upd, tenantID, assetID, mergedJSON); err != nil {
		return fmt.Errorf("merge tags: %w", err)
	}
	return nil
}

// Sink wraps a ClassificationStore + ClassifyCatalog into one
// classifier.ClassificationSink so cmd/ wires a single value.
type Sink struct {
	Store    *ClassificationStore
	Catalog  *ClassifyCatalog
}

func (s Sink) Apply(ctx context.Context, tenantID, assetID, classification, appliedBy string) error {
	return s.Store.Apply(ctx, tenantID, assetID, classification, appliedBy)
}

func (s Sink) MergeAssetTags(ctx context.Context, tenantID, assetID string, tags []string) error {
	return s.Catalog.MergeAssetTags(ctx, tenantID, assetID, tags)
}

// _ enforces interface conformance at compile time.
var (
	_ classifier.CatalogReader      = (*ClassifyCatalog)(nil)
	_ classifier.ClassificationSink = (*Sink)(nil)
)
