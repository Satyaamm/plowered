package http

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// GatewayAuthMW is an extra "below the application" auth layer. When a
// secret is configured, every request must carry it on the
// X-Gateway-Auth header — typically injected by nginx or by the
// frontend's BFF rewrite. Requests missing or mismatching the header
// get 401 before any other middleware (auth, rate-limit, audit) runs.
//
// Why this exists, given the platform already has session + bearer auth:
//   - Random scanners hitting /v1/auth/login probe the password handler
//     directly. Gateway-auth makes the entire HTTP surface invisible to
//     anyone who can't get the header — i.e. anyone not coming through
//     your reverse proxy or your frontend's BFF.
//   - A hosted deployment can rotate the gateway secret without
//     touching user sessions; brute-force pressure on the login form
//     vanishes immediately on rotation.
//   - The session cookie + bearer remain the real auth; this is a
//     defense-in-depth gate, not a replacement.
//
// Disabled when secret == "" so local dev + tests don't need to
// configure anything. Path prefixes in skipPaths (e.g. /healthz,
// /readyz, /metrics) bypass the check so load balancers + Prometheus
// scrapers don't need the secret.
//
// Constant-time comparison protects against timing oracles. The secret
// itself should come from the secrets vault in production (see
// PLOWERED_GATEWAY_SECRET in .env.production.example).
func GatewayAuthMW(secret string, skipPaths ...string) Middleware {
	secret = strings.TrimSpace(secret)
	skips := make([]string, 0, len(skipPaths))
	for _, p := range skipPaths {
		p = strings.TrimSpace(p)
		if p != "" {
			skips = append(skips, p)
		}
	}
	return func(next http.Handler) http.Handler {
		if secret == "" {
			// No-op middleware when no secret is configured. Returning
			// next unwrapped saves an indirection per request in dev
			// + tests.
			return next
		}
		secretBytes := []byte(secret)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, p := range skips {
				if strings.HasPrefix(r.URL.Path, p) {
					next.ServeHTTP(w, r)
					return
				}
			}
			got := r.Header.Get("X-Gateway-Auth")
			if got == "" || subtle.ConstantTimeCompare([]byte(got), secretBytes) != 1 {
				// Generic message — don't tell a scanner whether the
				// header was missing vs wrong. The honest signal goes
				// in the response body for the rare case an operator
				// hits this themselves.
				w.Header().Set("WWW-Authenticate", `Bearer realm="plowered-gateway"`)
				writeJSON(w, http.StatusUnauthorized, errorBody{
					"gateway_required",
					"this API requires a valid X-Gateway-Auth header — request via the configured frontend / proxy",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
