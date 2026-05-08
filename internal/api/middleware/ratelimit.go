package middleware

import (
	"context"
	"sync"

	"golang.org/x/time/rate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Satyaamm/plowered/internal/core/auth"
)

// RateLimitConfig sets per-tenant token-bucket parameters.
type RateLimitConfig struct {
	// PerSecond is the steady-state allowed rate.
	PerSecond float64
	// Burst is the bucket capacity (max requests allowed in a short spike).
	Burst int
	// SkipMethods bypass rate limiting (health probes, etc.).
	SkipMethods map[string]bool
}

// RateLimit enforces a per-tenant token bucket. Unauthenticated requests
// (no tenant on context) share a single anonymous bucket.
//
// Buckets are kept in memory and never evicted in v0; if the tenant
// cardinality grows beyond a few thousand, swap to an LRU.
func RateLimit(cfg RateLimitConfig) grpc.UnaryServerInterceptor {
	if cfg.PerSecond <= 0 {
		cfg.PerSecond = 50
	}
	if cfg.Burst <= 0 {
		cfg.Burst = 100
	}
	rl := &tenantLimiter{
		perSecond: rate.Limit(cfg.PerSecond),
		burst:     cfg.Burst,
		buckets:   make(map[string]*rate.Limiter),
	}
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if cfg.SkipMethods[info.FullMethod] {
			return handler(ctx, req)
		}
		key := "anonymous"
		if p, err := auth.PrincipalFromContext(ctx); err == nil && p.TenantID != "" {
			key = p.TenantID
		}
		if !rl.allow(key) {
			return nil, status.Error(codes.ResourceExhausted, "rate limit exceeded")
		}
		return handler(ctx, req)
	}
}

type tenantLimiter struct {
	perSecond rate.Limit
	burst     int
	mu        sync.Mutex
	buckets   map[string]*rate.Limiter
}

func (t *tenantLimiter) allow(key string) bool {
	t.mu.Lock()
	lim, ok := t.buckets[key]
	if !ok {
		lim = rate.NewLimiter(t.perSecond, t.burst)
		t.buckets[key] = lim
	}
	t.mu.Unlock()
	return lim.Allow()
}
