package middleware

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Satyaamm/plowered/internal/core/auth"
	"github.com/Satyaamm/plowered/internal/storage"
)

// Tenant copies the authenticated principal's tenant_id onto the context
// using the storage package's helper. Storage methods read tenant_id from
// context only — never from request payloads. This is the central control
// that makes cross-tenant data leakage impossible by construction
// (SECURITY.md §4).
//
// Must run after Auth.
func Tenant(skip map[string]bool) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if skip[info.FullMethod] {
			return handler(ctx, req)
		}
		p, err := auth.PrincipalFromContext(ctx)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "principal required")
		}
		if p.TenantID == "" {
			return nil, status.Error(codes.PermissionDenied, "tenant_id required")
		}
		ctx = storage.WithTenant(ctx, p.TenantID)
		return handler(ctx, req)
	}
}
