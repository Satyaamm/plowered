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
