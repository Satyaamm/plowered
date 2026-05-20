package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/cost"
)

// CostStore is the Postgres-backed cost.Store. Writes are
// fire-and-forget-but-logged from the caller's perspective —
// per-record dollar costs are tiny so the write contention is
// negligible compared to the underlying op we're tracking.
type CostStore struct {
	pool *pgxpool.Pool
}

func NewCostStore(p *pgxpool.Pool) *CostStore {
	return &CostStore{pool: p}
}

func (s *CostStore) Record(ctx context.Context, r cost.Record) error {
	if r.TenantID == "" || r.Kind == "" {
		return errors.New("cost: tenant + kind required")
	}
	attrJSON, _ := json.Marshal(r.Attributes)
	const q = `
		INSERT INTO cost_records (tenant_id, ts, kind, provider, cost_usd, attributes)
		VALUES ($1::uuid, COALESCE($2, now()), $3, $4, $5, $6)`
	var ts any
	if !r.TS.IsZero() {
		ts = r.TS
	}
	_, err := s.pool.Exec(ctx, q, r.TenantID, ts, string(r.Kind), r.Provider, r.CostUSD, attrJSON)
	if err != nil {
		return fmt.Errorf("insert cost record: %w", err)
	}
	return nil
}

func (s *CostStore) Recent(ctx context.Context, tenantID string, limit int) ([]*cost.Record, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	const q = `
		SELECT id::text, tenant_id::text, ts, kind, provider, cost_usd, attributes
		  FROM cost_records
		 WHERE tenant_id = $1::uuid
		 ORDER BY ts DESC
		 LIMIT $2`
	rows, err := s.pool.Query(ctx, q, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("recent cost records: %w", err)
	}
	defer rows.Close()
	out := []*cost.Record{}
	for rows.Next() {
		r, err := scanCostRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *CostStore) Daily(ctx context.Context, tenantID string, from, to time.Time) ([]*cost.DailyTotal, error) {
	if from.IsZero() {
		from = time.Now().UTC().AddDate(0, 0, -30)
	}
	if to.IsZero() {
		to = time.Now().UTC()
	}
	const q = `
		SELECT date_trunc('day', ts AT TIME ZONE 'UTC') AS day,
		       kind,
		       provider,
		       SUM(cost_usd)::float8,
		       COUNT(*)::bigint
		  FROM cost_records
		 WHERE tenant_id = $1::uuid
		   AND ts >= $2
		   AND ts <  $3
		 GROUP BY day, kind, provider
		 ORDER BY day ASC, kind, provider`
	rows, err := s.pool.Query(ctx, q, tenantID, from, to)
	if err != nil {
		return nil, fmt.Errorf("daily cost: %w", err)
	}
	defer rows.Close()
	out := []*cost.DailyTotal{}
	for rows.Next() {
		var d cost.DailyTotal
		var kind string
		if err := rows.Scan(&d.Day, &kind, &d.Provider, &d.CostUSD, &d.Count); err != nil {
			return nil, err
		}
		d.Kind = cost.Kind(kind)
		out = append(out, &d)
	}
	return out, rows.Err()
}

// GetBudget returns the per-tenant budget row; missing rows return a
// zero-value Budget with all fields blank (callers check MonthlyUSD).
func (s *CostStore) GetBudget(ctx context.Context, tenantID string) (*cost.Budget, error) {
	const q = `
		SELECT tenant_id::text, monthly_usd, warn_at_pct, hard_at_pct,
		       last_warned_at, last_hard_at, updated_at
		  FROM cost_budgets WHERE tenant_id = $1::uuid`
	b := &cost.Budget{TenantID: tenantID, WarnAtPct: 80, HardAtPct: 100}
	var monthly *float64
	var lastWarn, lastHard *time.Time
	err := s.pool.QueryRow(ctx, q, tenantID).Scan(
		&b.TenantID, &monthly, &b.WarnAtPct, &b.HardAtPct, &lastWarn, &lastHard, &b.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get budget: %w", err)
	}
	b.MonthlyUSD = monthly
	b.LastWarnedAt = lastWarn
	b.LastHardAt = lastHard
	return b, nil
}

func (s *CostStore) UpsertBudget(ctx context.Context, b *cost.Budget) (*cost.Budget, error) {
	if b == nil || b.TenantID == "" {
		return nil, errors.New("cost: nil/empty budget")
	}
	if b.WarnAtPct == 0 {
		b.WarnAtPct = 80
	}
	if b.HardAtPct == 0 {
		b.HardAtPct = 100
	}
	const q = `
		INSERT INTO cost_budgets (tenant_id, monthly_usd, warn_at_pct, hard_at_pct, updated_at)
		VALUES ($1::uuid, $2, $3, $4, now())
		ON CONFLICT (tenant_id) DO UPDATE
		   SET monthly_usd = EXCLUDED.monthly_usd,
		       warn_at_pct = EXCLUDED.warn_at_pct,
		       hard_at_pct = EXCLUDED.hard_at_pct,
		       updated_at  = now()
		RETURNING updated_at`
	if err := s.pool.QueryRow(ctx, q, b.TenantID, b.MonthlyUSD, b.WarnAtPct, b.HardAtPct).Scan(&b.UpdatedAt); err != nil {
		return nil, fmt.Errorf("upsert budget: %w", err)
	}
	return b, nil
}

func (s *CostStore) MarkWarned(ctx context.Context, tenantID string, at time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE cost_budgets SET last_warned_at = $2 WHERE tenant_id = $1::uuid`,
		tenantID, at,
	)
	return err
}

func (s *CostStore) MarkHard(ctx context.Context, tenantID string, at time.Time) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE cost_budgets SET last_hard_at = $2 WHERE tenant_id = $1::uuid`,
		tenantID, at,
	)
	return err
}

// TenantsWithCost yields every distinct tenant that has at least one
// row in cost_records. Used by the Watcher to scope its budget check
// (a tenant with no spend can't be over budget).
func (s *CostStore) TenantsWithCost(ctx context.Context) ([]string, error) {
	const q = `SELECT DISTINCT tenant_id::text FROM cost_records`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list tenants with cost: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *CostStore) RollingTotal(ctx context.Context, tenantID string, days int) (float64, error) {
	if days <= 0 {
		days = 30
	}
	const q = `
		SELECT COALESCE(SUM(cost_usd), 0)::float8
		  FROM cost_records
		 WHERE tenant_id = $1::uuid
		   AND ts >= now() - ($2::int * INTERVAL '1 day')`
	var total float64
	if err := s.pool.QueryRow(ctx, q, tenantID, days).Scan(&total); err != nil {
		return 0, fmt.Errorf("rolling total: %w", err)
	}
	return total, nil
}

func scanCostRecord(row pgx.Row) (*cost.Record, error) {
	r := &cost.Record{}
	var kind string
	var attrJSON []byte
	if err := row.Scan(&r.ID, &r.TenantID, &r.TS, &kind, &r.Provider, &r.CostUSD, &attrJSON); err != nil {
		return nil, err
	}
	r.Kind = cost.Kind(kind)
	if len(attrJSON) > 0 {
		_ = json.Unmarshal(attrJSON, &r.Attributes)
	}
	return r, nil
}
