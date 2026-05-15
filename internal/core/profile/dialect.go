package profile

import (
	"fmt"
	"strings"

	"github.com/Satyaamm/plowered/internal/core/connection"
)

// Dialect is the SQL-quirk layer. Profiling is conceptually identical
// across SQL warehouses (count, distinct, min, max) but the syntax
// drifts: identifier quoting differs, MAX on a TEXT column is
// supported everywhere but its NULL handling diverges, etc. Rather
// than ifing on connection.Type inside the query builder, we lift
// the divergence into this interface.
//
// Three implementations: PostgresDialect (also serves Redshift —
// Redshift is Postgres-wire-compatible), SnowflakeDialect, and
// MySQLDialect. Adding BigQuery / Athena later means one more file
// here, no changes to the Builder.
type Dialect interface {
	// Quote wraps an identifier (schema/table/column) appropriately.
	Quote(ident string) string
	// SampleClause returns the warehouse-native row-bounded sampling
	// syntax: "TABLESAMPLE BERNOULLI (1)" / "SAMPLE (10000 ROWS)" /
	// "LIMIT n" fallback. Returns an empty string if no special clause
	// applies (caller falls back to a wrapping LIMIT).
	SampleClause(rows int) string
	// SupportsApproxDistinct returns true if the dialect has a cheap
	// approximate-distinct function (Snowflake APPROX_COUNT_DISTINCT,
	// BigQuery APPROX_COUNT_DISTINCT). Falls back to COUNT(DISTINCT).
	SupportsApproxDistinct() bool
}

// PickDialect resolves a Dialect from a connection.Type. Returns an
// error for non-SQL types so the caller can short-circuit with a 400
// instead of silently returning empty profiles.
func PickDialect(t connection.Type) (Dialect, error) {
	switch t {
	case connection.TypePostgres, connection.TypeRedshift:
		return postgresDialect{}, nil
	case connection.TypeSnowflake:
		return snowflakeDialect{}, nil
	case connection.TypeMySQL:
		return mysqlDialect{}, nil
	default:
		if t.IsDocument() {
			return nil, fmt.Errorf("profile: cannot profile document source %q with SQL", t)
		}
		return nil, fmt.Errorf("profile: no dialect registered for %q", t)
	}
}

// Postgres / Redshift -------------------------------------------------

type postgresDialect struct{}

func (postgresDialect) Quote(ident string) string {
	// Reject embedded quotes — same defence as classifier's
	// quoteIdent: a hostile identifier in metadata would otherwise
	// inject SQL. Identifiers come from INFORMATION_SCHEMA reads but
	// belt-and-braces.
	if strings.ContainsAny(ident, `"`+"\x00") {
		return `""`
	}
	return `"` + ident + `"`
}

func (postgresDialect) SampleClause(rows int) string {
	return "" // fall back to outer LIMIT
}

func (postgresDialect) SupportsApproxDistinct() bool { return false }

// Snowflake -----------------------------------------------------------

type snowflakeDialect struct{}

func (snowflakeDialect) Quote(ident string) string {
	if strings.ContainsAny(ident, `"`+"\x00") {
		return `""`
	}
	return `"` + ident + `"`
}

func (snowflakeDialect) SampleClause(rows int) string {
	if rows <= 0 {
		return ""
	}
	return fmt.Sprintf("SAMPLE (%d ROWS)", rows)
}

func (snowflakeDialect) SupportsApproxDistinct() bool { return true }

// MySQL ---------------------------------------------------------------

type mysqlDialect struct{}

func (mysqlDialect) Quote(ident string) string {
	if strings.ContainsAny(ident, "`\x00") {
		return "``"
	}
	return "`" + ident + "`"
}

func (mysqlDialect) SampleClause(rows int) string {
	return "" // MySQL has no TABLESAMPLE; rely on outer LIMIT
}

func (mysqlDialect) SupportsApproxDistinct() bool { return false }
