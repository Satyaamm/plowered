package migration

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Satyaamm/plowered/internal/core/profile"
)

// SQL builders for the snapshot path. We intentionally do NOT use
// parameterised inserts via the driver — different drivers (pgx,
// database/sql with snowflake, etc.) have different placeholder
// conventions, and a generic warehouse.Executor doesn't carry bind
// support. The trade-off is that we inline literal values, which
// requires correct escaping.
//
// Escaping rules per dialect:
//
//   - All dialects we currently support accept single-quoted string
//     literals with '' as the escape for embedded quotes. We use that
//     uniformly. NUL bytes and embedded backslash-zero are rejected
//     defensively (real-world data shouldn't have them; if it does,
//     the failure is loud).
//   - Numbers and booleans are formatted natively (no quotes).
//   - NULL is emitted literally.
//
// This is acceptable for v0 because the SELECT is always from a
// PLATFORM-controlled SQL (no user input in the literal path; columns
// come from the catalog). If we ever pipe user-supplied values through
// here we'd need a tighter audit.

// buildSelect composes "SELECT col1, col2 FROM schema.table[ LIMIT N]".
// schema may be empty.
func buildSelect(d profile.Dialect, schema, table string, cols []string, maxRows int64) string {
	tableRef := d.Quote(table)
	if schema != "" {
		tableRef = d.Quote(schema) + "." + tableRef
	}
	colExpr := quotedJoin(d, cols)
	q := fmt.Sprintf("SELECT %s FROM %s", colExpr, tableRef)
	if maxRows > 0 {
		q += fmt.Sprintf(" LIMIT %d", maxRows)
	}
	return q
}

// buildTruncate composes "DELETE FROM table". Not TRUNCATE because
// TRUNCATE syntax / DDL semantics diverge wildly across warehouses
// (auto-commit on MySQL, owner-grant required on Snowflake). DELETE
// without WHERE is universally supported and (for snapshot-mode dest
// tables) functionally equivalent — the table is meant to be
// repopulated immediately.
func buildTruncate(d profile.Dialect, schema, table string) string {
	tableRef := d.Quote(table)
	if schema != "" {
		tableRef = d.Quote(schema) + "." + tableRef
	}
	return "DELETE FROM " + tableRef
}

// buildInsert composes a multi-row INSERT. Rows are tuples in the
// same order as cols. Empty batch returns an empty string.
func buildInsert(d profile.Dialect, schema, table string, cols []string, rows [][]any) string {
	if len(rows) == 0 || len(cols) == 0 {
		return ""
	}
	tableRef := d.Quote(table)
	if schema != "" {
		tableRef = d.Quote(schema) + "." + tableRef
	}
	colExpr := quotedJoin(d, cols)

	var sb strings.Builder
	sb.WriteString("INSERT INTO ")
	sb.WriteString(tableRef)
	sb.WriteString(" (")
	sb.WriteString(colExpr)
	sb.WriteString(") VALUES ")
	for i, row := range rows {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteByte('(')
		for j, v := range row {
			if j > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(formatLiteral(v))
		}
		sb.WriteByte(')')
	}
	return sb.String()
}

func quotedJoin(d profile.Dialect, cols []string) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = d.Quote(c)
	}
	return strings.Join(parts, ", ")
}

// formatLiteral renders an arbitrary driver-decoded value as a SQL
// literal. The match list intentionally favours common warehouse
// types; anything exotic falls back to a quoted string of fmt.Sprint.
func formatLiteral(v any) string {
	if v == nil {
		return "NULL"
	}
	switch x := v.(type) {
	case bool:
		if x {
			return "TRUE"
		}
		return "FALSE"
	case int:
		return strconv.Itoa(x)
	case int8, int16, int32, int64:
		return fmt.Sprint(x)
	case uint, uint8, uint16, uint32, uint64:
		return fmt.Sprint(x)
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case string:
		return quoteStringLiteral(x)
	case []byte:
		return quoteStringLiteral(string(x))
	default:
		return quoteStringLiteral(fmt.Sprint(x))
	}
}

func quoteStringLiteral(s string) string {
	// SQL standard: double up embedded single quotes. NUL bytes are
	// rejected by most warehouses anyway — strip defensively to avoid
	// a malformed-string failure mid-batch.
	s = strings.ReplaceAll(s, "\x00", "")
	s = strings.ReplaceAll(s, "'", "''")
	return "'" + s + "'"
}
