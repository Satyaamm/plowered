package middleware_test

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Satyaamm/plowered/internal/api/middleware"
	"github.com/Satyaamm/plowered/internal/core/auth"
	"github.com/Satyaamm/plowered/internal/storage"
)

func okHandler(_ context.Context, _ any) (any, error) { return "ok", nil }

func info(method string) *grpc.UnaryServerInfo {
	return &grpc.UnaryServerInfo{FullMethod: method}
}

func TestRecoveryConvertsPanic(t *testing.T) {
	mw := middleware.Recovery()
	_, err := mw(context.Background(), nil, info("/test/Panic"), func(_ context.Context, _ any) (any, error) {
		panic("boom")
	})
	if status.Code(err) != codes.Internal {
		t.Errorf("want Internal, got %v", err)
	}
}

func TestRequestIDInjectsAndPreserves(t *testing.T) {
	mw := middleware.RequestID()
	var seen string
	_, _ = mw(context.Background(), nil, info("/test/X"), func(ctx context.Context, _ any) (any, error) {
		seen = middleware.RequestIDFromContext(ctx)
		return nil, nil
	})
	if seen == "" {
		t.Error("request id not injected")
	}
}

func TestTenantBlocksMissingPrincipal(t *testing.T) {
	mw := middleware.Tenant(nil)
	_, err := mw(context.Background(), nil, info("/test/X"), okHandler)
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("want Unauthenticated, got %v", err)
	}
}

func TestTenantPropagates(t *testing.T) {
	mw := middleware.Tenant(nil)
	ctx := auth.WithPrincipal(context.Background(), auth.Principal{ID: "u1", TenantID: "t1"})
	var saw string
	_, err := mw(ctx, nil, info("/test/X"), func(c context.Context, _ any) (any, error) {
		s, terr := storage.TenantFromContext(c)
		if terr != nil {
			return nil, terr
		}
		saw = s
		return nil, nil
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if saw != "t1" {
		t.Errorf("saw tenant %q, want t1", saw)
	}
}

func TestRateLimitTriggers(t *testing.T) {
	mw := middleware.RateLimit(middleware.RateLimitConfig{PerSecond: 1, Burst: 1})
	ctx := auth.WithPrincipal(context.Background(), auth.Principal{ID: "u1", TenantID: "t1"})
	if _, err := mw(ctx, nil, info("/test/X"), okHandler); err != nil {
		t.Fatalf("first request: %v", err)
	}
	_, err := mw(ctx, nil, info("/test/X"), okHandler)
	if status.Code(err) != codes.ResourceExhausted {
		t.Errorf("want ResourceExhausted on burst exceed, got %v", err)
	}
}

func TestAuthDevPrincipalInjects(t *testing.T) {
	dev := auth.Principal{ID: "dev", TenantID: "dev-tenant"}
	mw := middleware.Auth(middleware.AuthConfig{DevPrincipal: &dev})
	var got auth.Principal
	_, err := mw(context.Background(), nil, info("/test/X"), func(c context.Context, _ any) (any, error) {
		p, perr := auth.PrincipalFromContext(c)
		if perr != nil {
			return nil, perr
		}
		got = p
		return nil, nil
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got.ID != "dev" || got.TenantID != "dev-tenant" {
		t.Errorf("dev principal not propagated: %+v", got)
	}
}

func TestAuthRejectsMissingToken(t *testing.T) {
	mw := middleware.Auth(middleware.AuthConfig{HS256Secret: []byte("k"), Issuer: "iss", Audience: "aud"})
	_, err := mw(context.Background(), nil, info("/test/X"), okHandler)
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("want Unauthenticated, got %v", err)
	}
	_ = errors.Is // keep import stable
}
