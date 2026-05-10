package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/quality"
	"github.com/Satyaamm/plowered/internal/storage"
)

// QualityStore is the Postgres-backed implementation of quality.Store.
type QualityStore struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewQualityStore(pool *pgxpool.Pool) *QualityStore {
	return &QualityStore{pool: pool, now: time.Now}
}

func (s *QualityStore) CreateCheck(ctx context.Context, c *quality.Check) (*quality.Check, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	cp := *c
	cp.TenantID = tenant
	if cp.ID == "" {
		cp.ID = newUUID()
	}
	now := s.now().UTC()
	cp.CreatedAt = now
	cp.UpdatedAt = now
	cfgJSON, _ := json.Marshal(cp.Config)

	const q = `
		INSERT INTO quality_checks (
			id, tenant_id, asset_id, asset_qn, name, type,
			config, severity, owner, enabled, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`
	_, err = s.pool.Exec(ctx, q,
		cp.ID, cp.TenantID, cp.AssetID, cp.AssetQN, cp.Name, string(cp.Type),
		cfgJSON, string(cp.Severity), cp.Owner, cp.Enabled, cp.CreatedAt, cp.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert check: %w", err)
	}
	return &cp, nil
}

func (s *QualityStore) UpdateCheck(ctx context.Context, c *quality.Check) (*quality.Check, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	cp := *c
	cp.TenantID = tenant
	cp.UpdatedAt = s.now().UTC()
	cfgJSON, _ := json.Marshal(cp.Config)
	tag, err := s.pool.Exec(ctx, `
		UPDATE quality_checks SET
			asset_id = $3, asset_qn = $4, name = $5, type = $6,
			config = $7, severity = $8, owner = $9, enabled = $10, updated_at = $11
		WHERE tenant_id = $1 AND id = $2`,
		tenant, cp.ID, cp.AssetID, cp.AssetQN, cp.Name, string(cp.Type),
		cfgJSON, string(cp.Severity), cp.Owner, cp.Enabled, cp.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("update check: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, quality.ErrNotFound
	}
	return &cp, nil
}

func (s *QualityStore) DeleteCheck(ctx context.Context, _ /* tenantID */, id string) error {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM quality_checks WHERE tenant_id = $1 AND id = $2`, tenant, id)
	if err != nil {
		return fmt.Errorf("delete check: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return quality.ErrNotFound
	}
	return nil
}

func (s *QualityStore) GetCheck(ctx context.Context, id string) (*quality.Check, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	row := s.pool.QueryRow(ctx, selectCheckSQL+` WHERE tenant_id = $1 AND id = $2`, tenant, id)
	return scanCheck(row)
}

func (s *QualityStore) ListChecks(ctx context.Context, _ string, assetID string) ([]*quality.Check, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	q := selectCheckSQL + ` WHERE tenant_id = $1`
	args := []any{tenant}
	if assetID != "" {
		q += ` AND asset_id = $2`
		args = append(args, assetID)
	}
	q += ` ORDER BY created_at`
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list checks: %w", err)
	}
	defer rows.Close()
	out := make([]*quality.Check, 0)
	for rows.Next() {
		c, err := scanCheck(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *QualityStore) RecordRun(ctx context.Context, r *quality.CheckRun) (*quality.CheckRun, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	cp := *r
	cp.TenantID = tenant
	if cp.ID == "" {
		cp.ID = newUUID()
	}
	if cp.StartedAt.IsZero() {
		cp.StartedAt = s.now().UTC()
	}
	if cp.FinishedAt.IsZero() {
		cp.FinishedAt = s.now().UTC()
	}
	propsJSON, _ := json.Marshal(cp.Properties)
	const q = `
		INSERT INTO quality_check_runs (
			id, tenant_id, check_id, asset_id, outcome, value, threshold,
			diagnostic, properties, severity, started_at, finished_at, duration_ms
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`
	_, err = s.pool.Exec(ctx, q,
		cp.ID, cp.TenantID, cp.CheckID, cp.AssetID, string(cp.Outcome),
		cp.Value, cp.Threshold, cp.Diagnostic, propsJSON, string(cp.Severity),
		cp.StartedAt, cp.FinishedAt, cp.Duration.Milliseconds(),
	)
	if err != nil {
		return nil, fmt.Errorf("record check_run: %w", err)
	}
	return &cp, nil
}

func (s *QualityStore) ListRuns(ctx context.Context, _ string, checkID string, limit int) ([]*quality.CheckRun, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	q := selectCheckRunSQL + ` WHERE tenant_id = $1`
	args := []any{tenant}
	if checkID != "" {
		q += ` AND check_id = $2`
		args = append(args, checkID)
	}
	q += fmt.Sprintf(` ORDER BY started_at DESC LIMIT %d`, limit)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list check_runs: %w", err)
	}
	defer rows.Close()
	out := make([]*quality.CheckRun, 0, limit)
	for rows.Next() {
		r, err := scanCheckRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

const selectCheckSQL = `
	SELECT id, tenant_id, asset_id, asset_qn, name, type,
	       config, severity, owner, enabled, created_at, updated_at
	  FROM quality_checks`

const selectCheckRunSQL = `
	SELECT id, tenant_id, check_id, asset_id, outcome, value, threshold,
	       diagnostic, properties, severity, started_at, finished_at, duration_ms
	  FROM quality_check_runs`

func scanCheck(row rowScanner) (*quality.Check, error) {
	var (
		c            quality.Check
		typ, sev     string
		cfgJSON      []byte
	)
	err := row.Scan(
		&c.ID, &c.TenantID, &c.AssetID, &c.AssetQN, &c.Name, &typ,
		&cfgJSON, &sev, &c.Owner, &c.Enabled, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, quality.ErrNotFound
		}
		return nil, err
	}
	c.Type = quality.CheckType(typ)
	c.Severity = quality.Severity(sev)
	if len(cfgJSON) > 0 {
		_ = json.Unmarshal(cfgJSON, &c.Config)
	}
	return &c, nil
}

func scanCheckRun(row rowScanner) (*quality.CheckRun, error) {
	var (
		r           quality.CheckRun
		outcome     string
		sev         string
		propsJSON   []byte
		durationMS  int64
	)
	err := row.Scan(
		&r.ID, &r.TenantID, &r.CheckID, &r.AssetID, &outcome, &r.Value, &r.Threshold,
		&r.Diagnostic, &propsJSON, &sev, &r.StartedAt, &r.FinishedAt, &durationMS,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, quality.ErrNotFound
		}
		return nil, err
	}
	r.Outcome = quality.Outcome(outcome)
	r.Severity = quality.Severity(sev)
	r.Duration = time.Duration(durationMS) * time.Millisecond
	if len(propsJSON) > 0 {
		_ = json.Unmarshal(propsJSON, &r.Properties)
	}
	return &r, nil
}
