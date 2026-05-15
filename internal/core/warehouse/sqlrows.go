package warehouse

import (
	"database/sql"
)

// sqlRowsAdapter wraps *sql.Rows to satisfy warehouse.Rows. The
// columns slice is captured once on construction so Columns() doesn't
// re-call sql.Rows.Columns(), which allocates each time.
type sqlRowsAdapter struct {
	rows    *sql.Rows
	columns []string
}

// fromSQLRows turns a *sql.Rows into our Rows abstraction. Returns
// the iteration error if column metadata couldn't be read up front.
func fromSQLRows(r *sql.Rows) (Rows, error) {
	cols, err := r.Columns()
	if err != nil {
		_ = r.Close()
		return nil, err
	}
	return &sqlRowsAdapter{rows: r, columns: cols}, nil
}

func (a *sqlRowsAdapter) Columns() []string { return a.columns }
func (a *sqlRowsAdapter) Next() bool        { return a.rows.Next() }
func (a *sqlRowsAdapter) Scan(dest ...any) error {
	return a.rows.Scan(dest...)
}
func (a *sqlRowsAdapter) Err() error   { return a.rows.Err() }
func (a *sqlRowsAdapter) Close() error { return a.rows.Close() }
