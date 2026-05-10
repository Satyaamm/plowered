package mcp

import (
	"context"

	"github.com/Satyaamm/plowered/internal/core/auth"
)

// We deliberately re-key the principal under this package so the MCP
// HTTP transport can stash the API key's mapped principal without
// importing internal/api/middleware. The handler reads it via Principal
// and falls back to the auth.PrincipalFromContext used by the rest of
// the platform when the HTTP path didn't run.
type mcpPrincipalKey struct{}

// WithPrincipal attaches a principal to ctx for downstream tool handlers.
// MCP transports call this; tools call Principal(ctx) to read it back.
func WithPrincipal(ctx context.Context, p auth.Principal) context.Context {
	return context.WithValue(ctx, mcpPrincipalKey{}, p)
}

// Principal returns the principal stored on ctx, falling back to the
// platform-wide auth.PrincipalFromContext key. When neither is present
// returns a zero Principal — handlers should still apply tenant scoping
// from storage.TenantFromContext.
func Principal(ctx context.Context) auth.Principal {
	if v := ctx.Value(mcpPrincipalKey{}); v != nil {
		if p, ok := v.(auth.Principal); ok {
			return p
		}
	}
	if p, err := auth.PrincipalFromContext(ctx); err == nil {
		return p
	}
	return auth.Principal{}
}
