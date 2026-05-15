package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/connection"
	"github.com/Satyaamm/plowered/internal/core/profile"
)

// ProfileStore is the Postgres implementation of profile.Cache plus
// profile.AssetReader. Both surfaces share a pool and the catalog's
// asset table; bundling them keeps cmd-wiring small (one constructor).
type ProfileStore struct {
	pool *pgxpool.Pool
}

func NewProfileStore(p *pgxpool.Pool) *ProfileStore {
	return &ProfileStore{pool: p}
}

// ----- profile.Cache --------------------------------------------------

func (s *ProfileStore) Get(ctx context.Context, tenantID, tableAssetID string) (*profile.Report, error) {
	const q = `
		SELECT body
		  FROM asset_profiles
		 WHERE tenant_id = $1::uuid AND table_asset_id = $2::uuid`
	var body []byte
	err := s.pool.QueryRow(ctx, q, tenantID, tableAssetID).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, profile.ErrNotCached
	}
	if err != nil {
		return nil, fmt.Errorf("read profile: %w", err)
	}
	var r profile.Report
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("decode profile: %w", err)
	}
	return &r, nil
}

func (s *ProfileStore) Put(ctx context.Context, report *profile.Report) error {
	if report == nil || report.TableAssetID == "" {
		return errors.New("profile: report missing table_asset_id")
	}
	body, err := json.Marshal(report)
	if err != nil {
		return err
	}
	// Tenant comes from the asset row itself — we look it up rather
	// than trusting caller-passed values. This is the same defence
	// every other tenant-scoped insert uses.
	const q = `
		INSERT INTO asset_profiles (tenant_id, table_asset_id, generated_at, body)
		SELECT a.tenant_id, a.id, $2, $3
		  FROM assets a
		 WHERE a.id = $1::uuid
		ON CONFLICT (tenant_id, table_asset_id) DO UPDATE
		   SET generated_at = EXCLUDED.generated_at,
		       body         = EXCLUDED.body`
	tag, err := s.pool.Exec(ctx, q, report.TableAssetID, report.GeneratedAt, body)
	if err != nil {
		return fmt.Errorf("write profile: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("profile: asset %s not found", report.TableAssetID)
	}
	return nil
}

// ----- profile.AssetReader -------------------------------------------

// ReadTable assembles the warehouse coordinates + column list for a
// table asset. The connection.Type is needed so the service picks the
// right SQL dialect.
func (s *ProfileStore) ReadTable(ctx context.Context, tenantID, tableAssetID string) (*profile.TableInfo, error) {
	const tableQ = `
		SELECT
		  COALESCE(a.properties->>'schema',''),
		  a.name,
		  c.id::text,
		  c.type
		FROM assets a
		JOIN connections c
		  ON c.tenant_id = a.tenant_id::uuid
		 AND a.qualified_name LIKE c.name || '.%'
		WHERE a.tenant_id = $1::uuid
		  AND a.id = $2::uuid
		  AND a.type = 'table'`
	info := &profile.TableInfo{}
	var typeStr string
	err := s.pool.QueryRow(ctx, tableQ, tenantID, tableAssetID).Scan(
		&info.Schema, &info.Table, &info.ConnectionID, &typeStr,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("profile: table asset %s not found", tableAssetID)
	}
	if err != nil {
		return nil, fmt.Errorf("read table: %w", err)
	}
	info.Type = connection.Type(typeStr)

	const colsQ = `
		WITH t AS (
			SELECT qualified_name FROM assets
			 WHERE tenant_id = $1::uuid AND id = $2::uuid
		)
		SELECT a.name,
		       COALESCE(a.properties->>'data_type','text')
		  FROM assets a, t
		 WHERE a.tenant_id = $1::uuid
		   AND a.type = 'column'
		   AND a.qualified_name LIKE t.qualified_name || '.%'
		 ORDER BY COALESCE((a.properties->>'ordinal_position')::int, 0), a.name`
	rows, err := s.pool.Query(ctx, colsQ, tenantID, tableAssetID)
	if err != nil {
		return nil, fmt.Errorf("read columns: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var c profile.ColumnSpec
		if err := rows.Scan(&c.Name, &c.DataType); err != nil {
			return nil, err
		}
		info.Columns = append(info.Columns, c)
	}
	return info, rows.Err()
}
