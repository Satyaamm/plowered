package classifier

import (
	"context"
	"errors"
	"fmt"

	"github.com/Satyaamm/plowered/internal/core/connection"
)

// ConnectionTypeResolver looks up the warehouse type for a connection.
// The orchestrator only knows tenant + connection IDs — it doesn't carry
// connection metadata around. The resolver is the smallest interface
// the multi-sampler needs from the connection store.
type ConnectionTypeResolver func(ctx context.Context, tenantID, connectionID string) (connection.Type, error)

// MultiSampler routes SampleTable calls to the per-warehouse sampler
// that matches the connection's Type. Unknown or unsupported types
// return an error that surfaces in the orchestrator's per-table
// Skipped counter rather than aborting the whole run.
type MultiSampler struct {
	ResolveType ConnectionTypeResolver
	Postgres    Sampler
	Snowflake   Sampler
	BigQuery    Sampler
}

func (m *MultiSampler) SampleTable(
	ctx context.Context,
	tenantID, connectionID, schema, table string,
	columns []string,
) ([]Result, error) {
	if m.ResolveType == nil {
		return nil, errors.New("classifier: connection type resolver not configured")
	}
	t, err := m.ResolveType(ctx, tenantID, connectionID)
	if err != nil {
		return nil, fmt.Errorf("resolve connection type: %w", err)
	}
	sampler := m.samplerFor(t)
	if sampler == nil {
		return nil, fmt.Errorf("classifier: unsupported connection type %q", t)
	}
	return sampler.SampleTable(ctx, tenantID, connectionID, schema, table, columns)
}

func (m *MultiSampler) samplerFor(t connection.Type) Sampler {
	switch t {
	case connection.TypePostgres:
		return m.Postgres
	case connection.TypeSnowflake:
		return m.Snowflake
	case connection.TypeBigQuery:
		return m.BigQuery
	default:
		return nil
	}
}
