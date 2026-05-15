package classifier

import (
	"context"
	"fmt"
	"strings"
)

// Sampler reads a small page of rows from a customer-owned table and
// runs every column's values through the detector pack. Implementations
// exist per warehouse type (Postgres, Snowflake, BigQuery) — see
// sampler_postgres.go, sampler_snowflake.go, sampler_bigquery.go. The
// orchestrator and the higher-level preview API both depend on this
// interface only; routing across warehouse types is the MultiSampler's
// job (sampler_multi.go).
type Sampler interface {
	SampleTable(
		ctx context.Context,
		tenantID, connectionID, schema, table string,
		columns []string,
	) ([]Result, error)
}

// ---------------------------------------------------------------------
// Shared helpers used by every per-warehouse sampler.
// ---------------------------------------------------------------------

// defaultSampleSize returns the per-table row budget when the caller
// didn't pin one explicitly. 100 is a sweet spot for regex precision
// without overloading a small warehouse.
func defaultSampleSize(n int) int {
	if n <= 0 {
		return 100
	}
	return n
}

// capColumns optionally trims the column slice to keep wide tables
// (>500 cols is real in some warehouses) from blowing the round-trip.
func capColumns(cols []string, limit int) []string {
	if limit > 0 && len(cols) > limit {
		return cols[:limit]
	}
	return cols
}

// quoteIdent wraps an identifier in double quotes, matching the SQL
// standard. Both Postgres and Snowflake accept this; BigQuery uses
// backticks (handled in its sampler). Identifiers containing a literal
// double-quote or NUL are rejected (returns empty string) — sampling
// untrusted DDL with embedded quotes is a SQL-injection vector.
func quoteIdent(s string) string {
	for _, r := range s {
		if r == '"' || r == '\x00' {
			return ""
		}
	}
	return "\"" + s + "\""
}

// joinQuoted renders a column list as "a", "b", "c" or "*" when the
// caller didn't supply names. Returned string slots directly into a
// SELECT.
func joinQuoted(columns []string) string {
	if len(columns) == 0 {
		return "*"
	}
	parts := make([]string, 0, len(columns))
	for _, c := range columns {
		q := quoteIdent(c)
		if q == "" {
			continue
		}
		parts = append(parts, q)
	}
	if len(parts) == 0 {
		return "*"
	}
	return strings.Join(parts, ", ")
}

// stringify renders an arbitrary driver-decoded value as a string so
// the detector regexes can match. Non-textual values fall through to
// fmt.Sprint.
func stringify(v any) string {
	if v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	default:
		return fmt.Sprint(v)
	}
}
