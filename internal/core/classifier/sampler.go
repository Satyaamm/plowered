package classifier

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Sampler reads a small page of rows from a Postgres table and runs every
// column's values through the detector pack. It returns one Result per
// column. The sampling is one round-trip per table — a single SELECT with
// all the column names — and is bounded by SampleSize.
type Sampler struct {
	// Dialer opens a *pgx.Conn for a (tenant, connection_id) pair. The
	// caller (transformrun executor / connection-classify handler) wires
	// this to the existing connection.Repo + secrets.Vault chain.
	Dialer func(ctx context.Context, tenantID, connectionID string) (*pgx.Conn, error)

	// SampleSize is the number of rows to read per table. Defaults to 100
	// when zero. The actual number returned can be smaller if the table
	// has fewer rows.
	SampleSize int

	// MaxColumnsPerTable caps the columns scanned per round to avoid
	// pathological wide tables. 0 = unlimited.
	MaxColumnsPerTable int
}

// SampleTable opens a connection, reads up to SampleSize rows from
// `<schema>.<table>`, and classifies every column independently. The
// `columns` argument is the set of column names to read; passing nil
// reads `*` and uses pgx's column metadata. Empty schema means the
// default search_path applies.
func (s *Sampler) SampleTable(
	ctx context.Context,
	tenantID, connectionID, schema, table string,
	columns []string,
) ([]Result, error) {
	if s.Dialer == nil {
		return nil, errors.New("classifier: dialer not configured")
	}
	size := s.SampleSize
	if size <= 0 {
		size = 100
	}
	if s.MaxColumnsPerTable > 0 && len(columns) > s.MaxColumnsPerTable {
		columns = columns[:s.MaxColumnsPerTable]
	}

	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	conn, err := s.Dialer(dialCtx, tenantID, connectionID)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("dial connection: %w", err)
	}
	defer conn.Close(context.Background())

	queryCtx, queryCancel := context.WithTimeout(ctx, 30*time.Second)
	defer queryCancel()

	q := buildSampleQuery(schema, table, columns, size)
	rows, err := conn.Query(queryCtx, q)
	if err != nil {
		return nil, fmt.Errorf("sample %s.%s: %w", schema, table, err)
	}
	defer rows.Close()

	fields := rows.FieldDescriptions()
	colNames := make([]string, len(fields))
	buckets := make([][]string, len(fields))
	for i, f := range fields {
		colNames[i] = string(f.Name)
		buckets[i] = make([]string, 0, size)
	}

	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		for i, v := range vals {
			s := stringify(v)
			if s == "" {
				continue
			}
			buckets[i] = append(buckets[i], s)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]Result, 0, len(colNames))
	for i, col := range colNames {
		out = append(out, ClassifySamples(col, buckets[i]))
	}
	return out, nil
}

// buildSampleQuery returns a SELECT that reads `size` rows. We avoid
// ORDER BY random() — on large tables it's a full table scan — and use
// TABLESAMPLE SYSTEM where possible. Postgres rejects TABLESAMPLE on
// views; we fall back to LIMIT for that case at runtime via a callback.
func buildSampleQuery(schema, table string, columns []string, size int) string {
	tableRef := quoteIdent(table)
	if schema != "" {
		tableRef = quoteIdent(schema) + "." + tableRef
	}
	cols := "*"
	if len(columns) > 0 {
		quoted := make([]string, len(columns))
		for i, c := range columns {
			quoted[i] = quoteIdent(c)
		}
		cols = strings.Join(quoted, ", ")
	}
	// LIMIT is the simplest universally-supported sampling. v0 is fine
	// with the bias of "first N rows in physical order" — column-level
	// regex matching cares less about row distribution than aggregate
	// stats would.
	return fmt.Sprintf("SELECT %s FROM %s LIMIT %d", cols, tableRef, size)
}

func quoteIdent(s string) string {
	// Disallow embedded quotes / spaces / control chars defensively.
	for _, r := range s {
		if r == '"' || r == '\x00' {
			return "\"\""
		}
	}
	return "\"" + s + "\""
}

// stringify renders a pgx-decoded value for regex matching. We're not
// trying to be exhaustive — non-textual values just get fmt.Sprint.
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
