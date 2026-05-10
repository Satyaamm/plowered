package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/lineage"
)

// ColumnLineageStore persists ColumnEdges into the column_lineage table.
// Edges arrive with qualified-name source/target tables; the store
// resolves them to asset UUIDs by looking up `assets.qualified_name`
// (with column suffix joined). Edges that don't resolve to existing
// assets are skipped — the caller is told via the count of misses.
type ColumnLineageStore struct {
	pool *pgxpool.Pool
}

func NewColumnLineageStore(p *pgxpool.Pool) *ColumnLineageStore {
	return &ColumnLineageStore{pool: p}
}

// Upsert resolves and inserts edges. tenantID is the multi-tenant key on
// every row; taskRunID, when non-empty, links each edge back to the
// task_run that produced it (so re-runs can be reasoned about). Returns
// the number of edges actually written and the number that didn't
// resolve (almost always: the upstream asset hasn't been crawled yet).
func (s *ColumnLineageStore) Upsert(
	ctx context.Context,
	tenantID, taskRunID string,
	edges []lineage.ColumnEdge,
) (written, misses int, err error) {
	if len(edges) == 0 {
		return 0, 0, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, e := range edges {
		// Skip edges that name no source — synthetic constants etc.
		// Column-lineage rows require both ends.
		if e.SourceTable == "" || e.SourceColumn == "" || e.TargetTable == "" || e.TargetColumn == "" {
			misses++
			continue
		}
		srcAssetID, err := lookupColumnAsset(ctx, tx, tenantID, e.SourceTable, e.SourceColumn)
		if err != nil {
			return written, misses, err
		}
		dstAssetID, err := lookupColumnAsset(ctx, tx, tenantID, e.TargetTable, e.TargetColumn)
		if err != nil {
			return written, misses, err
		}
		if srcAssetID == "" || dstAssetID == "" {
			misses++
			continue
		}
		const q = `
			INSERT INTO column_lineage (
				tenant_id, source_asset_id, source_column,
				target_asset_id, target_column,
				transform, expression, task_run_id
			) VALUES ($1::uuid, $2::uuid, $3, $4::uuid, $5, $6, $7, NULLIF($8,'')::uuid)
			ON CONFLICT (source_asset_id, source_column, target_asset_id, target_column)
			DO UPDATE SET transform = EXCLUDED.transform,
			              expression = EXCLUDED.expression,
			              task_run_id = EXCLUDED.task_run_id`
		if _, err := tx.Exec(ctx, q,
			tenantID, srcAssetID, e.SourceColumn,
			dstAssetID, e.TargetColumn,
			e.Transform, e.Expression, taskRunID,
		); err != nil {
			return written, misses, fmt.Errorf("insert column_lineage: %w", err)
		}
		written++
	}
	if err := tx.Commit(ctx); err != nil {
		return written, misses, fmt.Errorf("commit: %w", err)
	}
	return written, misses, nil
}

// ListByAsset returns all column-lineage edges that touch assetID, in
// either direction, joined to the asset row so callers can render
// names without a follow-up query.
func (s *ColumnLineageStore) ListByAsset(ctx context.Context, tenantID, assetID string) ([]ColumnLineageView, error) {
	const q = `
		SELECT cl.id::text,
		       cl.source_asset_id::text, src.qualified_name, cl.source_column,
		       cl.target_asset_id::text, tgt.qualified_name, cl.target_column,
		       cl.transform, cl.expression
		  FROM column_lineage cl
		  JOIN assets src ON src.id = cl.source_asset_id
		  JOIN assets tgt ON tgt.id = cl.target_asset_id
		 WHERE cl.tenant_id = $1::uuid
		   AND (cl.source_asset_id = $2::uuid OR cl.target_asset_id = $2::uuid
		        OR src.id IN (SELECT id FROM assets WHERE qualified_name LIKE (
		            SELECT qualified_name || '.%' FROM assets WHERE id = $2::uuid
		        ))
		        OR tgt.id IN (SELECT id FROM assets WHERE qualified_name LIKE (
		            SELECT qualified_name || '.%' FROM assets WHERE id = $2::uuid
		        ))
		   )
		 ORDER BY cl.id DESC
		 LIMIT 1000`
	rows, err := s.pool.Query(ctx, q, tenantID, assetID)
	if err != nil {
		return nil, fmt.Errorf("query column_lineage: %w", err)
	}
	defer rows.Close()
	var out []ColumnLineageView
	for rows.Next() {
		var v ColumnLineageView
		if err := rows.Scan(
			&v.ID,
			&v.SourceAssetID, &v.SourceQN, &v.SourceColumn,
			&v.TargetAssetID, &v.TargetQN, &v.TargetColumn,
			&v.Transform, &v.Expression,
		); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ColumnLineageView is the API-friendly shape for a column-level edge.
type ColumnLineageView struct {
	ID            string `json:"id"`
	SourceAssetID string `json:"source_asset_id"`
	SourceQN      string `json:"source_qualified_name"`
	SourceColumn  string `json:"source_column"`
	TargetAssetID string `json:"target_asset_id"`
	TargetQN      string `json:"target_qualified_name"`
	TargetColumn  string `json:"target_column"`
	Transform     string `json:"transform"`
	Expression    string `json:"expression,omitempty"`
}

// lookupColumnAsset finds the column-asset row whose qualified_name ends
// with `<table>.<column>` (case-insensitive). The crawler typically
// prefixes connection name + schema + table, but the SQL parser only
// sees schema.table — so the lookup uses a suffix match anchored on
// `.<table>.<column>` to bridge the two without losing tenant scope.
//
// When the parser sees a bare `column` (no table prefix), the query
// instead matches any `.<column>` ending — useful only when there's
// exactly one source so the executor pre-filled the table name; we
// keep this branch defensively for completeness.
//
// Returns "" when nothing matches; callers count that as a miss.
func lookupColumnAsset(ctx context.Context, tx pgx.Tx, tenantID, table, column string) (string, error) {
	if column == "" {
		return "", nil
	}
	column = strings.ToLower(column)
	if table == "" {
		const q = `SELECT id::text FROM assets
			WHERE tenant_id = $1
			  AND type = 'column'
			  AND lower(qualified_name) LIKE '%.' || $2
			LIMIT 1`
		var id string
		if err := tx.QueryRow(ctx, q, tenantID, column).Scan(&id); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return "", nil
			}
			return "", fmt.Errorf("lookup column %q: %w", column, err)
		}
		return id, nil
	}
	table = strings.ToLower(table)
	suffix := "%." + table + "." + column
	const q = `SELECT id::text FROM assets
		WHERE tenant_id = $1
		  AND type = 'column'
		  AND lower(qualified_name) LIKE $2
		ORDER BY length(qualified_name) ASC
		LIMIT 1`
	var id string
	if err := tx.QueryRow(ctx, q, tenantID, suffix).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Try exact match — handles cases where the SQL already
			// wrote a fully qualified name.
			const exact = `SELECT id::text FROM assets WHERE tenant_id = $1 AND lower(qualified_name) = $2 LIMIT 1`
			if err := tx.QueryRow(ctx, exact, tenantID, table+"."+column).Scan(&id); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return "", nil
				}
				return "", fmt.Errorf("lookup column %q: %w", table+"."+column, err)
			}
			return id, nil
		}
		return "", fmt.Errorf("lookup column %q: %w", suffix, err)
	}
	return id, nil
}
