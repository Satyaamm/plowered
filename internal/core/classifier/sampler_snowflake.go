package classifier

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SnowflakeSampler implements Sampler against a Snowflake warehouse via
// database/sql + the gosnowflake driver (registered by the cmd binary,
// not this package — so the classifier package stays driver-free).
//
// Snowflake supports a native row-bounded sampling clause that beats
// LIMIT for distribution quality:
//
//	SELECT col1, col2 FROM "db"."schema"."table" SAMPLE (200 ROWS)
//
// When the table is small (fewer rows than the sample size) Snowflake
// returns the whole table, which is what we want.
type SnowflakeSampler struct {
	// Dialer opens a *sql.DB for a (tenant, connection_id) pair. The
	// caller wires this to the existing connection.Repo + secrets.Vault
	// chain so the sampler never sees raw credentials. The *sql.DB is
	// owned by the dialer — the sampler does NOT call Close, since
	// gosnowflake's DBs are pooled and meant to be reused.
	Dialer func(ctx context.Context, tenantID, connectionID string) (*sql.DB, error)

	// SampleSize is the per-table row budget. 0 → defaultSampleSize().
	SampleSize int

	// MaxColumnsPerTable caps the columns scanned per round to bound the
	// width of pathological tables. 0 → unlimited.
	MaxColumnsPerTable int
}

func (s *SnowflakeSampler) SampleTable(
	ctx context.Context,
	tenantID, connectionID, schema, table string,
	columns []string,
) ([]Result, error) {
	if s.Dialer == nil {
		return nil, errors.New("classifier: snowflake dialer not configured")
	}
	size := defaultSampleSize(s.SampleSize)
	columns = capColumns(columns, s.MaxColumnsPerTable)

	dialCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	db, err := s.Dialer(dialCtx, tenantID, connectionID)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("dial connection: %w", err)
	}

	queryCtx, queryCancel := context.WithTimeout(ctx, 30*time.Second)
	defer queryCancel()

	q := buildSnowflakeSampleQuery(schema, table, columns, size)
	rows, err := db.QueryContext(queryCtx, q)
	if err != nil {
		return nil, fmt.Errorf("sample %s.%s: %w", schema, table, err)
	}
	defer rows.Close()

	colNames, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	buckets := make([][]string, len(colNames))
	for i := range colNames {
		buckets[i] = make([]string, 0, size)
	}

	// database/sql wants a pre-allocated slice of pointers per row.
	scanVals := make([]any, len(colNames))
	scanPtrs := make([]any, len(colNames))
	for i := range scanVals {
		scanPtrs[i] = &scanVals[i]
	}

	for rows.Next() {
		if err := rows.Scan(scanPtrs...); err != nil {
			return nil, err
		}
		for i, v := range scanVals {
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

// buildSnowflakeSampleQuery uses Snowflake's SAMPLE (N ROWS) clause.
// Identifiers stay quoted with double-quotes — Snowflake follows the
// SQL standard for these. Empty schema means the session's default
// schema is used (set in the DSN).
func buildSnowflakeSampleQuery(schema, table string, columns []string, size int) string {
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
	return fmt.Sprintf("SELECT %s FROM %s SAMPLE (%d ROWS)", cols, tableRef, size)
}
