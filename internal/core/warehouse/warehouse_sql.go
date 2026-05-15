package warehouse

import (
	"context"
	"database/sql"
	"fmt"
)

// sqlExecutor adapts any database/sql *sql.DB into our Executor. It's
// used for every warehouse whose driver speaks database/sql — that
// covers Snowflake, MySQL, Redshift (via Postgres driver), Databricks,
// and Athena (once wired). Postgres uses its own pgx-based executor
// because the rest of the codebase already uses pgx.Conn — see
// warehouse_postgres.go.
type sqlExecutor struct {
	db *sql.DB
}

// NewSQLExecutor wraps an already-dialed *sql.DB. The DB is NOT closed
// by Query — the caller (typically a per-call factory) decides the
// lifecycle. For Snowflake the DB pools internally; closing per-call
// is wasteful. For MySQL we close after the call since each call
// dials fresh.
func NewSQLExecutor(db *sql.DB) Executor {
	return &sqlExecutor{db: db}
}

func (e *sqlExecutor) Query(ctx context.Context, sqlText string) (Rows, error) {
	rows, err := e.db.QueryContext(ctx, sqlText)
	if err != nil {
		return nil, fmt.Errorf("warehouse: query: %w", err)
	}
	return fromSQLRows(rows)
}
