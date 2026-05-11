package http

import "net/http"

// SecurityHeadersMW sets the response headers SOC2 / ISO 27001 / OWASP
// security checklists expect. Each is a one-line declaration with a
// real-world threat model — kept verbose so the next reader doesn't
// have to look up what HSTS or X-Content-Type-Options actually buy us.
//
// In a production deployment behind TLS, set
// PLOWERED_SESSION_COOKIE_SECURE=1 so cookies match this posture.
func SecurityHeadersMW() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()

			// HSTS — force HTTPS for a year, including subdomains. Browsers
			// remember this for the duration; subsequent navigations to
			// http:// auto-upgrade. Only meaningful when the API actually
			// serves on HTTPS (i.e. behind a TLS-terminating LB).
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")

			// MIME-sniffing protection — without this a browser might
			// treat a JSON response as HTML and execute embedded
			// <script> tags if a content-type slips through.
			h.Set("X-Content-Type-Options", "nosniff")

			// Clickjacking protection. The API surface should never be
			// framed (we have no UI here). The web app sets its own
			// CSP frame-ancestors clause; this is defense-in-depth.
			h.Set("X-Frame-Options", "DENY")

			// Referrer leakage — don't include the full URL when a user
			// clicks an outbound link. "strict-origin-when-cross-origin"
			// sends only the origin off-site and full URL same-origin.
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")

			// Permissions Policy — the API serves no UI; lock down every
			// browser feature so a future XSS can't reach for the camera
			// or geolocation if a response is ever rendered as HTML.
			h.Set("Permissions-Policy",
				"camera=(), microphone=(), geolocation=(), interest-cohort=()")

			// Cross-origin isolation hints — browsers use these to decide
			// what cross-origin embeds and popups can do. The API has no
			// reason to allow either.
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			h.Set("Cross-Origin-Resource-Policy", "same-origin")

			// Content Security Policy — minimal because the API returns
			// JSON, not HTML. If someone navigates to /openapi.yaml or
			// /docs (Swagger UI), the Swagger HTML pulls scripts from
			// jsDelivr; allow that and nothing else.
			if r.URL.Path == "/docs" || r.URL.Path == "/openapi.yaml" {
				h.Set("Content-Security-Policy",
					"default-src 'self'; "+
						"script-src 'self' https://unpkg.com https://cdn.jsdelivr.net 'unsafe-inline'; "+
						"style-src 'self' https://unpkg.com https://cdn.jsdelivr.net 'unsafe-inline'; "+
						"img-src 'self' data:; "+
						"connect-src 'self'; "+
						"frame-ancestors 'none'")
			} else {
				h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
			}

			next.ServeHTTP(w, r)
		})
	}
}
