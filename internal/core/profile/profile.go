// Package profile computes per-column statistics for a catalogued
// table: null count, distinct count, min, max, top values, mean for
// numeric columns. The point is to make the catalog *legible* — a user
// who sees "users.email" should be able to glance at the column and
// know its shape without leaving the catalog.
//
// Design choices:
//
//   - One SELECT per table, not per column. Profiling a 200-column
//     wide table by issuing 200 queries crushes the source warehouse.
//     We emit one query that returns one row, packed with per-column
//     aggregates. Warehouses optimise this — Snowflake will scan the
//     micro-partitions once and project all columns in parallel.
//   - Results are CACHED. Profiling is read-heavy on the source; a UI
//     that re-profiles every catalog click would be a runaway cost.
//     We persist results in asset_profiles and serve them stale-while-
//     refreshing.
//   - SQL generation is dialect-aware. Postgres / Snowflake /
//     Redshift / MySQL each have small quirks (Snowflake's APPROX_COUNT
//     vs Postgres's COUNT DISTINCT, etc.). The Dialect interface
//     handles the divergence.
//
// Profile reports are bounded in size: top-N is configurable but
// capped at 10, samples are aggregated server-side, no raw rows leave
// the warehouse.
package profile

import (
	"time"
)

// Column is the per-column profile slice the UI renders. Pointer
// fields are nullable: a column with no numeric type has no Min/Max;
// a column that's 100% null has no Top.
type Column struct {
	Name           string         `json:"name"`
	DataType       string         `json:"data_type"`
	RowsSampled    int64          `json:"rows_sampled"`
	NullCount      int64          `json:"null_count"`
	DistinctCount  int64          `json:"distinct_count"`
	Min            *string        `json:"min,omitempty"`
	Max            *string        `json:"max,omitempty"`
	Mean           *float64       `json:"mean,omitempty"`
	TopValues      []TopValue     `json:"top_values,omitempty"`
}

// TopValue is one entry in the top-N list for a column.
type TopValue struct {
	Value string `json:"value"`
	Count int64  `json:"count"`
}

// Report is the full per-table profile result. Stored on
// asset_profiles keyed by table_asset_id; the API serialises it as-is.
type Report struct {
	TableAssetID string    `json:"table_asset_id"`
	Schema       string    `json:"schema"`
	Table        string    `json:"table"`
	GeneratedAt  time.Time `json:"generated_at"`
	RowsScanned  int64     `json:"rows_scanned"`
	Columns      []Column  `json:"columns"`
}
