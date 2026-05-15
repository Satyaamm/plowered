package profile

import (
	"fmt"
	"strings"
)

// ColumnSpec is the input shape for the builder: one entry per column
// the catalog wants profiled. DataType is the warehouse-reported type
// string (used to decide whether MIN/MAX/MEAN make sense — we never
// run AVG on a JSON column).
type ColumnSpec struct {
	Name     string
	DataType string
}

// BuildAggregateQuery composes ONE SELECT that returns one row whose
// columns are interleaved per-input-column aggregates:
//
//	col1_null_count, col1_distinct_count, col1_min, col1_max, col1_mean,
//	col2_null_count, col2_distinct_count, ...
//
// The caller knows the layout and scans positionally — see Service.Run
// for the decode side.
//
// Why one giant SELECT instead of N small ones: the warehouse scans
// the table once, projection is cheap, and the round-trip cost
// dominates for wide tables. Snowflake especially rewards this shape.
//
// sampleRows: rows to sample. If 0, scans the whole table (acceptable
// for small dev tables; the API caps this).
func BuildAggregateQuery(
	d Dialect,
	schema, table string,
	cols []ColumnSpec,
	sampleRows int,
) string {
	if len(cols) == 0 {
		return ""
	}

	tableRef := d.Quote(table)
	if schema != "" {
		tableRef = d.Quote(schema) + "." + tableRef
	}

	from := tableRef
	if clause := d.SampleClause(sampleRows); clause != "" {
		from = fmt.Sprintf("%s %s", tableRef, clause)
	} else if sampleRows > 0 {
		// Wrap in a sub-select with LIMIT — works on every dialect
		// even when no native sampling exists. Inner alias is "t".
		from = fmt.Sprintf("(SELECT * FROM %s LIMIT %d) t", tableRef, sampleRows)
	}

	parts := []string{"COUNT(*) AS __rows"}
	for _, c := range cols {
		q := d.Quote(c.Name)
		parts = append(parts,
			fmt.Sprintf("SUM(CASE WHEN %s IS NULL THEN 1 ELSE 0 END) AS %s",
				q, d.Quote(c.Name+"__nulls")),
		)
		if d.SupportsApproxDistinct() {
			parts = append(parts,
				fmt.Sprintf("APPROX_COUNT_DISTINCT(%s) AS %s",
					q, d.Quote(c.Name+"__distinct")))
		} else {
			parts = append(parts,
				fmt.Sprintf("COUNT(DISTINCT %s) AS %s",
					q, d.Quote(c.Name+"__distinct")))
		}
		if isComparable(c.DataType) {
			parts = append(parts,
				fmt.Sprintf("CAST(MIN(%s) AS TEXT) AS %s",
					q, d.Quote(c.Name+"__min")))
			parts = append(parts,
				fmt.Sprintf("CAST(MAX(%s) AS TEXT) AS %s",
					q, d.Quote(c.Name+"__max")))
		}
		if isNumeric(c.DataType) {
			parts = append(parts,
				fmt.Sprintf("AVG(CAST(%s AS DOUBLE PRECISION)) AS %s",
					q, d.Quote(c.Name+"__mean")))
		}
	}

	return fmt.Sprintf("SELECT %s FROM %s", strings.Join(parts, ", "), from)
}

// BuildTopValuesQuery returns one query per column: the top-N most-
// frequent values for that column. We deliberately don't pack these
// into the aggregate query — top-N requires GROUP BY + ORDER BY,
// which doesn't compose with single-row aggregates.
//
// Capped at N=10 rows per column; the call-site iterates columns
// and runs each.
func BuildTopValuesQuery(d Dialect, schema, table, column string, limit int) string {
	if limit <= 0 || limit > 10 {
		limit = 5
	}
	tableRef := d.Quote(table)
	if schema != "" {
		tableRef = d.Quote(schema) + "." + tableRef
	}
	q := d.Quote(column)
	return fmt.Sprintf(
		"SELECT CAST(%s AS TEXT) AS val, COUNT(*) AS cnt FROM %s "+
			"WHERE %s IS NOT NULL "+
			"GROUP BY %s ORDER BY cnt DESC LIMIT %d",
		q, tableRef, q, q, limit,
	)
}

// isComparable: types where MIN/MAX make sense. Text, dates, numbers
// all work; JSON / arrays / blobs don't.
func isComparable(dataType string) bool {
	t := strings.ToLower(dataType)
	if strings.Contains(t, "json") || strings.Contains(t, "array") ||
		strings.Contains(t, "blob") || strings.Contains(t, "geo") ||
		strings.Contains(t, "bytea") {
		return false
	}
	return true
}

// isNumeric: types where AVG makes sense.
func isNumeric(dataType string) bool {
	t := strings.ToLower(dataType)
	for _, prefix := range []string{
		"int", "float", "numeric", "decimal", "double", "real",
		"number", "money", "serial",
	} {
		if strings.Contains(t, prefix) {
			return true
		}
	}
	return false
}
