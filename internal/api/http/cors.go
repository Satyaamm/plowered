package http

import (
	"net/http"
	"strings"
)

// CORSConfig describes the cross-origin policy. In production both
// fields come from environment variables (PLOWERED_CORS_ORIGIN +
// PLOWERED_CORS_ALLOW_CREDENTIALS) so a deployment can flip them
// without rebuilding the binary.
type CORSConfig struct {
	// AllowedOrigins is the EXACT list of origins permitted to make
	// cross-origin requests. No wildcards — credentialled CORS forbids
	// them, and a wildcard here would defeat the same-origin
	// protections the rest of the stack relies on.
	//
	// Format: ["https://plowered.s2datasystems.in",
	//          "https://staging.plowered.s2datasystems.in"]
	AllowedOrigins []string

	// AllowCredentials decides whether to echo
	// Access-Control-Allow-Credentials: true. Required for cookie-
	// based session auth across subdomains; harmless when bearer auth
	// is in use too.
	AllowCredentials bool
}

// CORSMW returns a middleware that handles preflight OPTIONS requests
// and decorates every response with the right Access-Control-* headers.
//
// Behaviour:
//   - If the request's Origin is on the allow list, echo it back.
//   - If not, do not set any Access-Control-Allow-Origin header — the
//     browser will block the response, which is what we want.
//   - Always set Vary: Origin so caches keyed on URL alone don't serve
//     a CORS header to a different origin's request.
//   - On a preflight (OPTIONS + Access-Control-Request-Method), return
//     204 immediately with the relevant -Allow-Methods / -Allow-Headers
//     / -Max-Age. The downstream handler never sees the preflight.
//
// When AllowedOrigins is empty CORS is effectively disabled — useful
// for single-origin deployments (compose-stack dev) where the
// frontend and backend share an origin.
func CORSMW(cfg CORSConfig, _skip ...string) Middleware {
	allow := make(map[string]bool, len(cfg.AllowedOrigins))
	for _, o := range cfg.AllowedOrigins {
		o = strings.TrimRight(strings.TrimSpace(o), "/")
		if o != "" {
			allow[o] = true
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			h := w.Header()
			// Vary: Origin is required on every response, not just on
			// matched ones. A cache that ignored Origin would otherwise
			// serve plowered's headers to evil.example.com.
			h.Add("Vary", "Origin")

			if origin != "" && allow[origin] {
				h.Set("Access-Control-Allow-Origin", origin)
				if cfg.AllowCredentials {
					h.Set("Access-Control-Allow-Credentials", "true")
				}
				// Echo back the requested headers + methods on a
				// preflight; the browser uses these to decide whether
				// to send the real request.
				if r.Method == http.MethodOptions &&
					r.Header.Get("Access-Control-Request-Method") != "" {
					if m := r.Header.Get("Access-Control-Request-Method"); m != "" {
						h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
						_ = m // documents that we accept whatever the browser asks for in the list above
					}
					if rh := r.Header.Get("Access-Control-Request-Headers"); rh != "" {
						// Echoing the requested headers verbatim is the
						// simplest path — browsers only ask for headers
						// the calling JS actually set, and our app
						// already authenticates via Authorization /
						// Cookie / X-Gateway-Auth.
						h.Set("Access-Control-Allow-Headers", rh)
					} else {
						h.Set("Access-Control-Allow-Headers",
							"Content-Type, Authorization, X-Gateway-Auth, X-Request-ID")
					}
					// Browsers cache the preflight for this many seconds
					// before re-asking. 10 min is the sweet spot — short
					// enough that policy changes propagate within one
					// active session, long enough to dodge per-request
					// preflight cost on a chatty UI.
					h.Set("Access-Control-Max-Age", "600")
					w.WriteHeader(http.StatusNoContent)
					return
				}
				// Expose headers the frontend reads from JS. The
				// rate-limit + request-id headers are the only ones
				// non-default JS code needs visibility into.
				h.Set("Access-Control-Expose-Headers",
					"RateLimit-Limit, RateLimit-Remaining, RateLimit-Reset, X-Request-ID")
			} else if r.Method == http.MethodOptions &&
				r.Header.Get("Access-Control-Request-Method") != "" {
				// Preflight from a disallowed origin: short-circuit
				// with 403. Without this, the browser would still get
				// a useful error but the handler would burn cycles
				// running auth + body decode for a request the browser
				// will then refuse to surface.
				w.WriteHeader(http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
