package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/quality"
)

// PoolDataSource adapts a pgxpool.Pool to quality.DataSource. It runs a
// single scalar / time query and returns the first column of the first
// row. Identifiers in the SQL must be quoted by the caller — quality
// checks already template them in.
//
// PoolDataSource is the v0 datasource: it points at *Plowered's own*
// Postgres for self-tests and embedded mode. Per-tenant connection
// resolution (Snowflake, BigQuery, customer Postgres, …) lands in a
// follow-up that introduces a connections registry.
type PoolDataSource struct {
	Pool *pgxpool.Pool

	// SamplePercent applies TABLESAMPLE BERNOULLI to FROM-clauses when set
	// (per the SamplingDataSource contract). 0 disables sampling.
	SamplePercent float64
}

// NewPoolDataSource builds a DataSource over the supplied pool.
func NewPoolDataSource(pool *pgxpool.Pool) *PoolDataSource {
	return &PoolDataSource{Pool: pool}
}

// QueryScalar runs sql and returns the first column of the first row.
func (d *PoolDataSource) QueryScalar(ctx context.Context, sql string, args ...any) (any, error) {
	row := d.Pool.QueryRow(ctx, sql, args...)
	var v any
	if err := row.Scan(&v); err != nil {
		return nil, fmt.Errorf("pool datasource: scan: %w", err)
	}
	return v, nil
}

// QueryTime runs sql and returns the first column as a time.Time.
func (d *PoolDataSource) QueryTime(ctx context.Context, sql string, args ...any) (time.Time, error) {
	row := d.Pool.QueryRow(ctx, sql, args...)
	var t time.Time
	if err := row.Scan(&t); err != nil {
		return time.Time{}, fmt.Errorf("pool datasource: scan time: %w", err)
	}
	return t, nil
}

// WithSample satisfies quality.SamplingDataSource. Callers re-template the
// SQL in concrete check implementations; this is a placeholder for when
// we wire the SQL rewriter (a follow-up that intercepts FROM clauses).
func (d *PoolDataSource) WithSample(percent float64) quality.DataSource {
	cp := *d
	cp.SamplePercent = percent
	return &cp
}
