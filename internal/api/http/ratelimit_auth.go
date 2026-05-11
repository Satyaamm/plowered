package http

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// clientIP lives in audit_mw.go (same package); we reuse it.

// AuthRateLimitMW caps how often any single IP can hit auth endpoints
// (login, signup, accept-invite, resend-verification). Bots that try
// password spraying or signup floods get 429'd after a small burst.
//
// Implementation notes:
//
//   - Per-IP token bucket. 5 requests / minute with a burst of 8.
//     Generous enough that a human typo doesn't get locked out, tight
//     enough that brute-force is impractical.
//   - In-process map keyed by IP. Single-instance deployments are
//     covered; horizontal scale needs a Redis bucket — left as a TODO
//     since we're still single-node today.
//   - A janitor goroutine prunes idle entries every 10 minutes so the
//     map can't grow forever under a slow scan.
//   - Only the four "auth write" paths are limited. /v1/auth/me and
//     /v1/auth/logout stay unlimited so a busy SPA doesn't get
//     throttled refreshing its session.
type authRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*ipBucket
	rps     rate.Limit
	burst   int
}

type ipBucket struct {
	limiter *rate.Limiter
	seenAt  time.Time
}

func newAuthRateLimiter(perMinute, burst int) *authRateLimiter {
	r := &authRateLimiter{
		buckets: make(map[string]*ipBucket),
		rps:     rate.Every(time.Minute / time.Duration(perMinute)),
		burst:   burst,
	}
	go r.janitor()
	return r
}

func (r *authRateLimiter) janitor() {
	t := time.NewTicker(10 * time.Minute)
	for range t.C {
		cutoff := time.Now().Add(-30 * time.Minute)
		r.mu.Lock()
		for ip, b := range r.buckets {
			if b.seenAt.Before(cutoff) {
				delete(r.buckets, ip)
			}
		}
		r.mu.Unlock()
	}
}

// limitState is the snapshot returned with each request — the
// middleware copies it into the RateLimit-* response headers so clients
// can pace themselves without polling for 429.
type limitState struct {
	limit     int
	remaining int
	resetSec  int
	allowed   bool
}

func (r *authRateLimiter) check(ip string) limitState {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.buckets[ip]
	if !ok {
		b = &ipBucket{limiter: rate.NewLimiter(r.rps, r.burst)}
		r.buckets[ip] = b
	}
	b.seenAt = time.Now()
	res := b.limiter.Reserve()
	allowed := res.OK() && res.Delay() == 0
	if !allowed && res.OK() {
		res.Cancel() // don't consume a future token slot on rejection
	}
	tokens := math.Floor(b.limiter.Tokens())
	if tokens < 0 {
		tokens = 0
	}
	// Time until the bucket refills back to 1 token (= when the next
	// allowed call becomes possible). Math: 1 token / rps seconds.
	resetSec := int(math.Ceil(float64(time.Second) / float64(r.rps) / float64(time.Second)))
	if resetSec < 1 {
		resetSec = 1
	}
	return limitState{
		limit:     r.burst,
		remaining: int(tokens),
		resetSec:  resetSec,
		allowed:   allowed,
	}
}

// AuthRateLimitMW returns a middleware that caps the four credential-
// mutation endpoints. Other paths pass through untouched.
func AuthRateLimitMW(perMinute, burst int) Middleware {
	if perMinute <= 0 {
		perMinute = 5
	}
	if burst <= 0 {
		burst = 8
	}
	limited := map[string]bool{
		"/v1/auth/login":                true,
		"/v1/auth/signup":               true,
		"/v1/auth/accept-invite":        true,
		"/v1/auth/resend-verification":  true,
		"/v1/auth/forgot-password":      true,
		"/v1/auth/reset-password":       true,
	}
	rl := newAuthRateLimiter(perMinute, burst)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !limited[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}
			st := rl.check(clientIP(r))
			// IETF draft-ietf-httpapi-ratelimit-headers (now RFC 9239).
			// Clients (curl, browsers, SDKs) read these to pace
			// themselves instead of probing for 429.
			w.Header().Set("RateLimit-Limit", strconv.Itoa(st.limit))
			w.Header().Set("RateLimit-Remaining", strconv.Itoa(st.remaining))
			w.Header().Set("RateLimit-Reset", strconv.Itoa(st.resetSec))
			if !st.allowed {
				w.Header().Set("Retry-After", strconv.Itoa(st.resetSec))
				writeJSON(w, http.StatusTooManyRequests, errorBody{
					"rate_limited",
					"too many attempts — try again in a minute",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

