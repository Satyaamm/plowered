// Package warehouse defines the single SQL execution surface every
// SQL-capable source (Postgres, Snowflake, MySQL, Redshift, BigQuery,
// Athena, Databricks) implements once. Higher-level services —
// profiling, text-to-SQL, migration — depend on this interface only.
// They never know which warehouse type they're talking to.
//
// Why this lives in its own package, not as another method on
// connection.Tester: testers do a single round-trip ping; the things
// we want to do here are streaming rows for arbitrary SELECTs. Two
// different responsibilities, two different interfaces.
package warehouse

import (
	"context"
	"errors"
)

// Executor runs a single SELECT and returns its rows. Implementations
// per warehouse are wired through the registry below. The caller never
// closes the underlying driver connection — the executor owns its own
// dial lifecycle so individual call-sites stay free of pool plumbing.
//
// Streaming vs materialised: implementations may stream (preferred for
// large result sets) or buffer (acceptable for profiling round-trips
// that bound rows themselves). The Rows type abstracts both shapes.
type Executor interface {
	// Query runs sql and returns a Rows iterator. The caller MUST call
	// Rows.Close even when iterating to completion — implementations
	// hold a network connection until Close.
	Query(ctx context.Context, sql string) (Rows, error)
}

// Rows iterates a result set. The shape matches database/sql.Rows
// closely so adapting from existing drivers is trivial, but we don't
// embed sql.Rows because non-database/sql drivers (BigQuery, Athena)
// need to fit too.
type Rows interface {
	// Columns returns the column names in selection order.
	Columns() []string
	// Next advances to the next row; returns false at end or on error
	// (call Err to distinguish).
	Next() bool
	// Scan copies the current row into dest. dest must have len ==
	// len(Columns()). Each dest[i] is a pointer to any.
	Scan(dest ...any) error
	// Err returns the iteration error, if any.
	Err() error
	// Close releases driver resources.
	Close() error
}

// Factory builds an Executor for a given (tenant, connection_id). The
// factory closure handles credential lookup + dial — call-sites just
// receive a ready Executor.
//
// Implementations live in this package alongside the per-warehouse
// drivers (warehouse_postgres.go, warehouse_snowflake.go, etc.) and
// register themselves via Register at package-init time of the
// adapter wiring layer (cmd/plowered/main.go calls the constructor
// functions there).
type Factory func(ctx context.Context, tenantID, connectionID string) (Executor, error)

// MultiFactory dispatches to a per-warehouse Factory by connection
// Type. The Resolver supplies the Type for a connection ID; the
// concrete factories are registered against connection.Type strings.
type MultiFactory struct {
	Resolver ConnectionTypeResolver
	byType   map[string]Factory
}

// ConnectionTypeResolver is the smallest interface MultiFactory needs
// from the connection store: "what type is this connection?". Mirrors
// the resolver used by the classifier so call-sites can share it.
type ConnectionTypeResolver func(ctx context.Context, tenantID, connectionID string) (string, error)

// NewMultiFactory wires an empty dispatcher; register per-warehouse
// factories via Register before use.
func NewMultiFactory(r ConnectionTypeResolver) *MultiFactory {
	return &MultiFactory{Resolver: r, byType: map[string]Factory{}}
}

// Register attaches a Factory to a connection Type string. Calling
// Register twice with the same Type overrides — useful in tests.
func (m *MultiFactory) Register(typ string, f Factory) {
	if m.byType == nil {
		m.byType = map[string]Factory{}
	}
	m.byType[typ] = f
}

// Open builds an Executor for the given connection. Returns
// ErrUnsupportedType for sources that don't have an SQL surface.
func (m *MultiFactory) Open(ctx context.Context, tenantID, connectionID string) (Executor, error) {
	if m.Resolver == nil {
		return nil, errors.New("warehouse: connection type resolver not configured")
	}
	typ, err := m.Resolver(ctx, tenantID, connectionID)
	if err != nil {
		return nil, err
	}
	f, ok := m.byType[typ]
	if !ok {
		return nil, ErrUnsupportedType
	}
	return f(ctx, tenantID, connectionID)
}

// Errors --------------------------------------------------------------

// ErrUnsupportedType is returned when a caller tries to open an
// Executor for a connection whose Type has no registered factory
// (typical for document sources or for cloud sources whose driver
// isn't compiled in).
var ErrUnsupportedType = errors.New("warehouse: connection type has no SQL executor")

// ErrDriverNotInstalled is returned by stub executors whose driver is
// excluded from the build (BigQuery, Athena, etc.). HTTP layer should
// surface this as "driver not available in this build" rather than 500.
var ErrDriverNotInstalled = errors.New("warehouse: driver not installed in this build")
