package classifier

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// PostgresSampler implements Sampler against a real Postgres warehouse.
// It opens one short-lived connection per table, runs a single SELECT
// with the requested column list, and classifies every column from the
// resulting rows. Sampling strategy is intentionally simple — LIMIT N
// in physical order — because the detectors care about presence in the
// column distribution, not statistical sampling rigor.
type PostgresSampler struct {
	// Dialer opens a *pgx.Conn for a (tenant, connection_id) pair. The
	// caller wires this to the existing connection.Repo + secrets.Vault
	// chain so the sampler never sees raw credentials.
	Dialer func(ctx context.Context, tenantID, connectionID string) (*pgx.Conn, error)

	// SampleSize is the per-table row budget. 0 → defaultSampleSize().
	SampleSize int

	// MaxColumnsPerTable caps the columns scanned per round to bound the
	// width of pathological tables. 0 → unlimited.
	MaxColumnsPerTable int
}

func (s *PostgresSampler) SampleTable(
	ctx context.Context,
	tenantID, connectionID, schema, table string,
	columns []string,
) ([]Result, error) {
	if s.Dialer == nil {
		return nil, errors.New("classifier: postgres dialer not configured")
	}
	size := defaultSampleSize(s.SampleSize)
	columns = capColumns(columns, s.MaxColumnsPerTable)

	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	conn, err := s.Dialer(dialCtx, tenantID, connectionID)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("dial connection: %w", err)
	}
	defer conn.Close(context.Background())

	queryCtx, queryCancel := context.WithTimeout(ctx, 30*time.Second)
	defer queryCancel()

	q := buildPostgresSampleQuery(schema, table, columns, size)
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
			str := stringify(v)
			if str == "" {
				continue
			}
			buckets[i] = append(buckets[i], str)
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

// buildPostgresSampleQuery returns a SELECT that reads `size` rows.
// We avoid ORDER BY random() — on large tables it's a full table scan.
// LIMIT is universally supported (works on tables, views, and matviews)
// and "first N rows in physical order" is acceptable for column-level
// regex classification, which cares about presence not distribution.
func buildPostgresSampleQuery(schema, table string, columns []string, size int) string {
	tableRef := quoteIdent(table)
	if schema != "" {
		tableRef = quoteIdent(schema) + "." + tableRef
	}
	cols := "*"
	if len(columns) > 0 {
		quoted := make([]string, 0, len(columns))
		for _, c := range columns {
			q := quoteIdent(c)
			if q == "" {
				continue
			}
			quoted = append(quoted, q)
		}
		if len(quoted) > 0 {
			cols = strings.Join(quoted, ", ")
		}
	}
	return fmt.Sprintf("SELECT %s FROM %s LIMIT %d", cols, tableRef, size)
}
