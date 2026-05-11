package http

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Satyaamm/plowered/internal/core/audit"
	"github.com/Satyaamm/plowered/internal/core/auth"
)

// AuditMW emits an audit event for every authenticated request — both
// reads and writes. The middleware sits AFTER AuthMW + TenantMW so the
// principal is on the context.
//
// Best practice (HIPAA §164.312(b), SOC 2 CC6, GDPR Art. 30): capture
// who/what/when on every interaction, not just mutations. Reads are
// noisy; we keep cardinality bounded by recording the URL pattern, not
// the path-with-IDs (URL parameters are still in the audit row's
// resource_type/resource_id where the handler set them).
func AuditMW(writer audit.Writer, serviceName, serviceVersion string, skipPrefixes ...string) Middleware {
	return func(next http.Handler) http.Handler {
		if writer == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, p := range skipPrefixes {
				if strings.HasPrefix(r.URL.Path, p) {
					next.ServeHTTP(w, r)
					return
				}
			}
			start := time.Now()
			rec := &auditStatusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			pr, _ := auth.PrincipalFromContext(r.Context())
			if pr.TenantID == "" {
				return // unauthenticated paths fell through
			}
			id, _ := r.Context().Value(requestIDKey{}).(string)

			outcome := audit.OutcomeSuccess
			switch {
			case rec.status == http.StatusUnauthorized || rec.status == http.StatusForbidden:
				outcome = audit.OutcomeDenied
			case rec.status >= 400:
				outcome = audit.OutcomeFailure
			}
			action := actionFromHTTP(r.Method, r.URL.Path)

			_ = writer.Emit(r.Context(), audit.Event{
				TenantID:     pr.TenantID,
				ActorID:      pr.ID,
				ActorKind:    "user",
				Action:       action,
				ResourceType: resourceTypeFromPath(r.URL.Path),
				IP:           clientIP(r),
				UserAgent:    r.Header.Get("User-Agent"),
				RequestID:    id,
				HTTPMethod:   r.Method,
				HTTPPath:     r.URL.Path,
				HTTPStatus:   rec.status,
				Outcome:      outcome,
				ServiceName:  serviceName,
				ServiceVer:   serviceVersion,
				CreatedAt:    start.UTC(),
			})
		})
	}
}

// actionFromHTTP turns "GET /v1/assets" into "asset.read",
// "POST /v1/assets" into "asset.create", etc. Falls back to
// "<method>.<resource>" for routes we don't recognise.
func actionFromHTTP(method, path string) string {
	resource := resourceTypeFromPath(path)
	if resource == "" {
		resource = "unknown"
	}
	switch method {
	case http.MethodGet:
		return resource + ".read"
	case http.MethodPost:
		// POST commonly creates, but `:trigger` / `:run` style suffixes are
		// commands rather than creates. Detect them.
		if strings.Contains(path, "/trigger") || strings.Contains(path, "/run") {
			return resource + ".execute"
		}
		if strings.Contains(path, "/restore") {
			return resource + ".restore"
		}
		return resource + ".create"
	case http.MethodPatch, http.MethodPut:
		return resource + ".update"
	case http.MethodDelete:
		return resource + ".delete"
	}
	return strings.ToLower(method) + "." + resource
}

// resourceTypeFromPath extracts the second path segment of a /v1/<x>/...
// URL. Returns "" when the path doesn't match the convention.
func resourceTypeFromPath(path string) string {
	parts := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 4)
	if len(parts) < 2 || parts[0] != "v1" {
		return ""
	}
	res := parts[1]
	// Trim trailing punctuation like "assets:search" → "assets".
	if i := strings.IndexAny(res, ":?#"); i >= 0 {
		res = res[:i]
	}
	// Naive singularization for the most common nouns.
	for _, suf := range []string{"ies", "s"} {
		if strings.HasSuffix(res, suf) {
			if suf == "ies" {
				return res[:len(res)-3] + "y"
			}
			return res[:len(res)-1]
		}
	}
	return res
}

func clientIP(r *http.Request) string {
	if h := r.Header.Get("X-Forwarded-For"); h != "" {
		// First entry is the original client; the rest is the proxy chain.
		if i := strings.IndexByte(h, ','); i >= 0 {
			return strings.TrimSpace(h[:i])
		}
		return strings.TrimSpace(h)
	}
	if h := r.Header.Get("X-Real-IP"); h != "" {
		return strings.TrimSpace(h)
	}
	// RemoteAddr is "host:port" — strip the port so the rate-limit
	// bucket and audit row see the same address across reconnects.
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

type auditStatusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *auditStatusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Flush delegates so SSE handlers can flush through the audit wrapper.
func (s *auditStatusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
