package http

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/Satyaamm/plowered/internal/core/auth"
)

// APIRateLimitMW caps authenticated traffic per principal. The IP-level
// AuthRateLimitMW protects sign-in; this one protects the authenticated
// surface against a leaked token / API key being used to hammer the
// platform.
//
// Strategy: per-principal token bucket. Read-side (GET) gets a fatter
// burst because the catalog UI legitimately makes ~10 requests on
// every page load. Write-side (everything else) is tighter.
//
// Industry baselines for reference (per minute, per principal):
//
//	Stripe API:        100 read, 100 write
//	GitHub REST:       5000/hour authenticated
//	OpenAI:            3500 req/min for the chat tier
//	Slack:             tier 2/3/4 vary 20-100/min
//
// We default to 120 reads + 30 writes/min — plenty of headroom for a
// real workspace, lethal for a runaway script.
type apiRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*apiBucket
	readR   rate.Limit
	writeR  rate.Limit
	readB   int
	writeB  int
}

type apiBucket struct {
	read   *rate.Limiter
	write  *rate.Limiter
	seenAt time.Time
}

func newAPIRateLimiter(readPerMin, writePerMin int) *apiRateLimiter {
	rl := &apiRateLimiter{
		buckets: make(map[string]*apiBucket),
		readR:   rate.Every(time.Minute / time.Duration(readPerMin)),
		writeR:  rate.Every(time.Minute / time.Duration(writePerMin)),
		readB:   readPerMin / 2,  // half-minute headroom
		writeB:  writePerMin / 2,
	}
	if rl.readB < 10 {
		rl.readB = 10
	}
	if rl.writeB < 5 {
		rl.writeB = 5
	}
	go rl.janitor()
	return rl
}

func (r *apiRateLimiter) janitor() {
	t := time.NewTicker(10 * time.Minute)
	for range t.C {
		cutoff := time.Now().Add(-30 * time.Minute)
		r.mu.Lock()
		for k, b := range r.buckets {
			if b.seenAt.Before(cutoff) {
				delete(r.buckets, k)
			}
		}
		r.mu.Unlock()
	}
}

func (r *apiRateLimiter) check(key string, write bool) limitState {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.buckets[key]
	if !ok {
		b = &apiBucket{
			read:  rate.NewLimiter(r.readR, r.readB),
			write: rate.NewLimiter(r.writeR, r.writeB),
		}
		r.buckets[key] = b
	}
	b.seenAt = time.Now()
	limiter := b.read
	limit := r.readB
	per := r.readR
	if write {
		limiter = b.write
		limit = r.writeB
		per = r.writeR
	}
	res := limiter.Reserve()
	allowed := res.OK() && res.Delay() == 0
	if !allowed && res.OK() {
		res.Cancel()
	}
	tokens := math.Floor(limiter.Tokens())
	if tokens < 0 {
		tokens = 0
	}
	resetSec := int(math.Ceil(float64(time.Second) / float64(per) / float64(time.Second)))
	if resetSec < 1 {
		resetSec = 1
	}
	return limitState{
		limit:     limit,
		remaining: int(tokens),
		resetSec:  resetSec,
		allowed:   allowed,
	}
}

// APIRateLimitMW returns a middleware that caps authenticated traffic
// per principal. Methods other than GET/HEAD/OPTIONS hit the tighter
// write bucket.
//
// Reads default 120/min, writes 30/min. Skip paths bypass the limiter
// entirely so health checks and OpenAPI fetches aren't throttled.
func APIRateLimitMW(readPerMin, writePerMin int, skipPrefixes ...string) Middleware {
	if readPerMin <= 0 {
		readPerMin = 120
	}
	if writePerMin <= 0 {
		writePerMin = 30
	}
	rl := newAPIRateLimiter(readPerMin, writePerMin)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, p := range skipPrefixes {
				if len(r.URL.Path) >= len(p) && r.URL.Path[:len(p)] == p {
					next.ServeHTTP(w, r)
					return
				}
			}
			// Key by principal when authed, fall back to IP. The IP key
			// is rarely hit in practice because this middleware sits
			// AFTER auth — anonymous requests have already been 401'd.
			key := clientIP(r)
			if p, err := auth.PrincipalFromContext(r.Context()); err == nil && p.ID != "" {
				key = "u:" + p.ID
			}
			isWrite := r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
			st := rl.check(key, isWrite)
			w.Header().Set("RateLimit-Limit", strconv.Itoa(st.limit))
			w.Header().Set("RateLimit-Remaining", strconv.Itoa(st.remaining))
			w.Header().Set("RateLimit-Reset", strconv.Itoa(st.resetSec))
			if !st.allowed {
				w.Header().Set("Retry-After", strconv.Itoa(st.resetSec))
				writeJSON(w, http.StatusTooManyRequests, errorBody{
					"rate_limited",
					"per-principal rate limit hit — slow down",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
