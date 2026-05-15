package warehouse

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// pgxExecutor adapts a pgx.Conn into our Executor. pgx is preferred
// over database/sql for Postgres because the rest of the codebase
// already uses pgx (the existing classifier sampler, transform tasks,
// connection store all dial via pgx). Sharing the same driver avoids
// driver-quirk surprises.
//
// Lifecycle: the executor takes ownership of the connection and
// closes it on Rows.Close (the rows iterator holds the conn open
// until then).
type pgxExecutor struct {
	conn *pgx.Conn
}

// NewPostgresExecutor wraps an already-connected *pgx.Conn. The
// connection will be closed when the resulting Rows.Close() is
// called — callers should not close the conn directly.
func NewPostgresExecutor(conn *pgx.Conn) Executor {
	return &pgxExecutor{conn: conn}
}

func (e *pgxExecutor) Query(ctx context.Context, sqlText string) (Rows, error) {
	rows, err := e.conn.Query(ctx, sqlText)
	if err != nil {
		_ = e.conn.Close(context.Background())
		return nil, fmt.Errorf("warehouse: query: %w", err)
	}
	return &pgxRows{rows: rows, conn: e.conn}, nil
}

type pgxRows struct {
	rows pgx.Rows
	conn *pgx.Conn
	cols []string
}

func (r *pgxRows) Columns() []string {
	if r.cols != nil {
		return r.cols
	}
	fds := r.rows.FieldDescriptions()
	r.cols = make([]string, len(fds))
	for i, fd := range fds {
		r.cols[i] = string(fd.Name)
	}
	return r.cols
}

func (r *pgxRows) Next() bool { return r.rows.Next() }

// Scan: pgx wants typed pointers; database/sql wants any. We bridge by
// calling rows.Values() and copying into the destination any pointers.
// Slightly slower than a true typed scan but lets the upstream code
// stay driver-agnostic.
func (r *pgxRows) Scan(dest ...any) error {
	vals, err := r.rows.Values()
	if err != nil {
		return err
	}
	if len(vals) != len(dest) {
		return fmt.Errorf("warehouse: scan: got %d cols, dest has %d", len(vals), len(dest))
	}
	for i, v := range vals {
		ptr, ok := dest[i].(*any)
		if !ok {
			return fmt.Errorf("warehouse: scan: dest[%d] must be *any", i)
		}
		*ptr = v
	}
	return nil
}

func (r *pgxRows) Err() error { return r.rows.Err() }

func (r *pgxRows) Close() error {
	r.rows.Close()
	return r.conn.Close(context.Background())
}
