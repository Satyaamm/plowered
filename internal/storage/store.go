// Package storage defines the persistence interface used by the graph engine.
// Implementations live under internal/storage/<backend>/.
//
// Tenant isolation: every method takes a context.Context that MUST carry a
// tenant_id (extracted by the TenantInterceptor at the API edge). Stores are
// responsible for filtering by tenant_id on every query — handlers cannot
// pass it explicitly. This is the central control that makes cross-tenant
// data leakage impossible by construction.
package storage

import (
	"context"
	"errors"

	"github.com/Satyaamm/plowered/internal/core/graph"
)

// Store is the persistence-agnostic API the graph engine talks to.
// Implementations: memory (tests/dev), sqlite (single binary), postgres (prod).
type Store interface {
	// Assets
	CreateAsset(ctx context.Context, a *graph.Asset) (*graph.Asset, error)
	GetAsset(ctx context.Context, id string) (*graph.Asset, error)
	GetAssetByQualifiedName(ctx context.Context, qualifiedName string) (*graph.Asset, error)
	UpdateAsset(ctx context.Context, a *graph.Asset) (*graph.Asset, error)
	DeleteAsset(ctx context.Context, id string) error
	ListAssets(ctx context.Context, opts ListAssetsOptions) ([]*graph.Asset, string, error)

	// Edges
	CreateEdge(ctx context.Context, e *graph.Edge) (*graph.Edge, error)
	DeleteEdge(ctx context.Context, id string) error
	Neighbors(ctx context.Context, assetID string, opts NeighborsOptions) ([]*graph.Edge, error)

	// Lifecycle
	Ping(ctx context.Context) error
	Close() error
}

type ListAssetsOptions struct {
	Type      graph.AssetType
	ParentID  string
	PageSize  int
	PageToken string
}

type NeighborsOptions struct {
	Kind     graph.EdgeKind // "" for any
	Outgoing bool           // true = source==assetID, false = target==assetID
	Limit    int
}

// TenantFromContext extracts the tenant id placed by the TenantInterceptor.
// Stores call this at the top of every method.
func TenantFromContext(ctx context.Context) (string, error) {
	v := ctx.Value(tenantKey{})
	if v == nil {
		return "", graph.ErrTenantMissing
	}
	t, ok := v.(string)
	if !ok || t == "" {
		return "", graph.ErrTenantMissing
	}
	return t, nil
}

// WithTenant attaches a tenant id to ctx. Used by the TenantInterceptor and
// by tests. Application code should not call this directly.
func WithTenant(ctx context.Context, tenantID string) context.Context {
	return context.WithValue(ctx, tenantKey{}, tenantID)
}

type tenantKey struct{}

// ErrUnsupported is returned when a backend cannot satisfy the requested
// operation (e.g. SQLite asked for column-level lineage walks).
var ErrUnsupported = errors.New("storage: operation not supported by this backend")
