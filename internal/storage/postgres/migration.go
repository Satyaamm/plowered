package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	mig "github.com/Satyaamm/plowered/internal/core/migration"
)

// MigrationStore is the Postgres-backed mig.Store. Both Plan
// CRUD and Run history live here — collapsing them into one struct
// keeps cmd-wiring small and matches the pattern used by
// ClassificationStore + ProfileStore.
type MigrationStore struct {
	pool *pgxpool.Pool
}

func NewMigrationStore(p *pgxpool.Pool) *MigrationStore {
	return &MigrationStore{pool: p}
}

// ----- Plans ----------------------------------------------------------

func (s *MigrationStore) CreatePlan(ctx context.Context, p *mig.Plan) (*mig.Plan, error) {
	if p == nil {
		return nil, errors.New("migration: nil plan")
	}
	mapJSON, err := json.Marshal(p.ColumnMap)
	if err != nil {
		return nil, fmt.Errorf("encode column_map: %w", err)
	}
	const q = `
		INSERT INTO migration_plans
			(tenant_id, name,
			 source_connection_id, source_schema, source_table,
			 dest_connection_id,   dest_schema,   dest_table,
			 column_map, mode, write_mode, cursor_column, created_by)
		VALUES ($1::uuid, $2,
		        $3::uuid, $4, $5,
		        $6::uuid, $7, $8,
		        $9::jsonb, $10, $11, $12, NULLIF($13,'')::uuid)
		RETURNING id::text, created_at, updated_at`
	row := s.pool.QueryRow(ctx, q,
		p.TenantID, p.Name,
		p.SourceConnectionID, p.SourceSchema, p.SourceTable,
		p.DestConnectionID, p.DestSchema, p.DestTable,
		mapJSON, string(p.Mode), string(p.WriteMode), p.CursorColumn, p.CreatedBy,
	)
	if err := row.Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, fmt.Errorf("insert plan: %w", err)
	}
	return p, nil
}

func (s *MigrationStore) GetPlan(ctx context.Context, tenantID, planID string) (*mig.Plan, error) {
	const q = `
		SELECT id::text, tenant_id::text, name,
		       source_connection_id::text, source_schema, source_table,
		       dest_connection_id::text,   dest_schema,   dest_table,
		       column_map, mode, write_mode, cursor_column,
		       COALESCE(created_by::text,''), created_at, updated_at
		  FROM migration_plans
		 WHERE tenant_id = $1::uuid AND id = $2::uuid`
	row := s.pool.QueryRow(ctx, q, tenantID, planID)
	return scanPlan(row)
}

func (s *MigrationStore) ListPlans(ctx context.Context, tenantID string) ([]*mig.Plan, error) {
	const q = `
		SELECT id::text, tenant_id::text, name,
		       source_connection_id::text, source_schema, source_table,
		       dest_connection_id::text,   dest_schema,   dest_table,
		       column_map, mode, write_mode, cursor_column,
		       COALESCE(created_by::text,''), created_at, updated_at
		  FROM migration_plans
		 WHERE tenant_id = $1::uuid
		 ORDER BY created_at DESC`
	rows, err := s.pool.Query(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list plans: %w", err)
	}
	defer rows.Close()
	var out []*mig.Plan
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *MigrationStore) DeletePlan(ctx context.Context, tenantID, planID string) error {
	const q = `DELETE FROM migration_plans WHERE tenant_id = $1::uuid AND id = $2::uuid`
	_, err := s.pool.Exec(ctx, q, tenantID, planID)
	return err
}

// ----- Runs -----------------------------------------------------------

func (s *MigrationStore) StartRun(ctx context.Context, tenantID, planID string) (*mig.Run, error) {
	const q = `
		INSERT INTO migration_runs (tenant_id, plan_id, status)
		VALUES ($1::uuid, $2::uuid, 'running')
		RETURNING id::text, tenant_id::text, plan_id::text, status, started_at`
	row := s.pool.QueryRow(ctx, q, tenantID, planID)
	r := &mig.Run{}
	var statusStr string
	if err := row.Scan(&r.ID, &r.TenantID, &r.PlanID, &statusStr, &r.StartedAt); err != nil {
		return nil, fmt.Errorf("start run: %w", err)
	}
	r.Status = mig.RunStatus(statusStr)
	return r, nil
}

func (s *MigrationStore) FinishRun(
	ctx context.Context,
	tenantID, runID string,
	status mig.RunStatus,
	rowsRead, rowsWritten int64,
	errStr string,
) error {
	const q = `
		UPDATE migration_runs
		   SET status       = $3,
		       finished_at  = now(),
		       rows_read    = $4,
		       rows_written = $5,
		       error        = NULLIF($6,'')
		 WHERE tenant_id = $1::uuid AND id = $2::uuid`
	_, err := s.pool.Exec(ctx, q, tenantID, runID, string(status), rowsRead, rowsWritten, errStr)
	return err
}

func (s *MigrationStore) ListRuns(ctx context.Context, tenantID, planID string) ([]*mig.Run, error) {
	const q = `
		SELECT id::text, tenant_id::text, plan_id::text, status,
		       started_at, finished_at, rows_read, rows_written,
		       COALESCE(checkpoint_uri,''), COALESCE(error,'')
		  FROM migration_runs
		 WHERE tenant_id = $1::uuid AND plan_id = $2::uuid
		 ORDER BY started_at DESC`
	rows, err := s.pool.Query(ctx, q, tenantID, planID)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()
	var out []*mig.Run
	for rows.Next() {
		r := &mig.Run{}
		var statusStr string
		var finishedAt *time.Time
		if err := rows.Scan(
			&r.ID, &r.TenantID, &r.PlanID, &statusStr,
			&r.StartedAt, &finishedAt, &r.RowsRead, &r.RowsWritten,
			&r.CheckpointURI, &r.Error,
		); err != nil {
			return nil, err
		}
		r.Status = mig.RunStatus(statusStr)
		r.FinishedAt = finishedAt
		out = append(out, r)
	}
	return out, rows.Err()
}

func scanPlan(row pgx.Row) (*mig.Plan, error) {
	p := &mig.Plan{}
	var modeStr, writeMode string
	var mapJSON []byte
	err := row.Scan(
		&p.ID, &p.TenantID, &p.Name,
		&p.SourceConnectionID, &p.SourceSchema, &p.SourceTable,
		&p.DestConnectionID, &p.DestSchema, &p.DestTable,
		&mapJSON, &modeStr, &writeMode, &p.CursorColumn,
		&p.CreatedBy, &p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, mig.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan plan: %w", err)
	}
	p.Mode = mig.Mode(modeStr)
	p.WriteMode = mig.WriteMode(writeMode)
	if len(mapJSON) > 0 {
		if err := json.Unmarshal(mapJSON, &p.ColumnMap); err != nil {
			return nil, fmt.Errorf("decode column_map: %w", err)
		}
	}
	return p, nil
}
