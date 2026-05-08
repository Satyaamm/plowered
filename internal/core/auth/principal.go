// Package auth holds the principal type and helpers for extracting the
// authenticated identity from a request context. Token verification itself
// lives in internal/api/middleware/auth.
package auth

import (
	"context"
	"errors"
)

// Principal is the authenticated caller of an RPC. Service code must treat
// the Principal as the source of truth for "who is doing this", never
// re-reading it from request bodies or headers.
type Principal struct {
	ID       string
	Email    string
	TenantID string
	Roles    []string
	Groups   []string
}

var ErrNoPrincipal = errors.New("auth: principal missing from context")

type principalKey struct{}

func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

func PrincipalFromContext(ctx context.Context) (Principal, error) {
	v := ctx.Value(principalKey{})
	if v == nil {
		return Principal{}, ErrNoPrincipal
	}
	p, ok := v.(Principal)
	if !ok || p.ID == "" {
		return Principal{}, ErrNoPrincipal
	}
	return p, nil
}

// HasRole reports whether the principal has been granted the given role.
func (p Principal) HasRole(role string) bool {
	for _, r := range p.Roles {
		if r == role {
			return true
		}
	}
	return false
}
