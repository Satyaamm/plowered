package http

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Satyaamm/plowered/internal/core/auth"
	"github.com/Satyaamm/plowered/internal/storage"
)

// Chain composes HTTP middlewares left-to-right (outermost first). The
// returned handler runs Recovery → RequestID → Logging → Auth → Tenant →
// inner.
type Middleware func(http.Handler) http.Handler

func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// RecoveryMW converts panics into 500 responses. Top of the chain.
func RecoveryMW(logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.ErrorContext(r.Context(), "panic in http handler",
						"path", r.URL.Path, "panic", rec)
					writeJSON(w, http.StatusInternalServerError, errorBody{"internal", "internal error"})
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// RequestIDMW honors the X-Request-ID header or assigns a fresh value.
type requestIDKey struct{}

func RequestIDMW() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := r.Header.Get("X-Request-ID")
			if id == "" {
				id = newRequestID()
			}
			ctx := context.WithValue(r.Context(), requestIDKey{}, id)
			w.Header().Set("X-Request-ID", id)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func newRequestID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "req-fallback"
	}
	return hex.EncodeToString(b[:])
}

// LoggingMW emits one structured line per request.
func LoggingMW(logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := &statusRecorder{ResponseWriter: w, status: 200}
			next.ServeHTTP(ww, r)

			id, _ := r.Context().Value(requestIDKey{}).(string)
			level := slog.LevelInfo
			if ww.status >= 500 {
				level = slog.LevelError
			} else if ww.status >= 400 {
				level = slog.LevelWarn
			}
			tenant := ""
			if p, perr := auth.PrincipalFromContext(r.Context()); perr == nil {
				tenant = p.TenantID
			}
			logger.LogAttrs(r.Context(), level, "http",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", ww.status),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
				slog.String("request_id", id),
				slog.String("tenant_id", tenant),
			)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// AuthMW verifies the Authorization bearer token using the supplied verifier
// and places the resulting Principal on the request context. Skipped paths
// (/healthz, /readyz, /v1/auth/*) bypass auth entirely.
type TokenVerifier func(token string) (auth.Principal, error)

func AuthMW(verify TokenVerifier, skipPrefixes ...string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, p := range skipPrefixes {
				if strings.HasPrefix(r.URL.Path, p) {
					next.ServeHTTP(w, r)
					return
				}
			}
			h := r.Header.Get("Authorization")
			const prefix = "Bearer "
			if !strings.HasPrefix(h, prefix) {
				writeJSON(w, http.StatusUnauthorized, errorBody{"unauthenticated", "Bearer token required"})
				return
			}
			p, err := verify(strings.TrimPrefix(h, prefix))
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, errorBody{"unauthenticated", err.Error()})
				return
			}
			ctx := auth.WithPrincipal(r.Context(), p)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// TenantMW reads the principal placed by AuthMW and attaches its tenant_id
// onto the storage context. Storage methods read it from there only.
func TenantMW(skipPrefixes ...string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, p := range skipPrefixes {
				if strings.HasPrefix(r.URL.Path, p) {
					next.ServeHTTP(w, r)
					return
				}
			}
			p, err := auth.PrincipalFromContext(r.Context())
			if err != nil || p.TenantID == "" {
				writeJSON(w, http.StatusUnauthorized, errorBody{"tenant_required", "tenant_id missing from token"})
				return
			}
			ctx := storage.WithTenant(r.Context(), p.TenantID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// CORSMW is a permissive CORS configuration for local development. Production
// deploys override allowed origins via the API server config.
func CORSMW(allowedOrigins []string) Middleware {
	allow := func(origin string) bool {
		for _, o := range allowedOrigins {
			if o == "*" || o == origin {
				return true
			}
		}
		return false
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin != "" && allow(origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Vary", "Origin")
				w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,DELETE,OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Request-ID")
				w.Header().Set("Access-Control-Max-Age", "600")
			}
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
