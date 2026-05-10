package http

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Satyaamm/plowered/internal/core/auth"
	"github.com/Satyaamm/plowered/internal/core/identity"
)

// SessionAuthMW resolves the caller via either:
//   1. a session cookie (set by /v1/auth/login), or
//   2. a Bearer JWT token (legacy / SDK / service-to-service)
//
// On success it places the Principal on the request context exactly the
// way AuthMW does, so all downstream code (TenantMW, audit, handlers)
// keeps working unchanged.
//
// skipPrefixes matches AuthMW: anything under /healthz, /readyz, /metrics,
// /v1/auth/* etc. bypasses auth.
func SessionAuthMW(repo identity.Repo, cookieName string, bearer TokenVerifier, skipPrefixes ...string) Middleware {
	if cookieName == "" {
		cookieName = "plowered_session"
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, p := range skipPrefixes {
				if strings.HasPrefix(r.URL.Path, p) {
					next.ServeHTTP(w, r)
					return
				}
			}

			// 1. Session cookie path.
			if c, err := r.Cookie(cookieName); err == nil && c.Value != "" && repo != nil {
				p, ok := principalFromSession(r.Context(), repo, c.Value)
				if ok {
					next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), p)))
					return
				}
				// Cookie was present but invalid — clear it so the browser
				// doesn't keep sending a dead session.
				clearSessionCookie(w, AuthConfig{CookieName: cookieName})
			}

			// 2. Bearer token fallback (existing JWT verifier).
			h := r.Header.Get("Authorization")
			const prefix = "Bearer "
			if strings.HasPrefix(h, prefix) && bearer != nil {
				p, err := bearer(strings.TrimPrefix(h, prefix))
				if err == nil {
					next.ServeHTTP(w, r.WithContext(auth.WithPrincipal(r.Context(), p)))
					return
				}
			}

			writeJSON(w, http.StatusUnauthorized, errorBody{"unauthenticated",
				"sign in to access this resource"})
		})
	}
}

func principalFromSession(ctx context.Context, repo identity.Repo, sessionID string) (auth.Principal, bool) {
	sess, err := repo.GetSession(ctx, sessionID)
	if err != nil {
		return auth.Principal{}, false
	}
	if !sess.Active(time.Now()) {
		return auth.Principal{}, false
	}
	user, err := repo.GetUserByID(ctx, sess.UserID)
	if err != nil || !user.IsEmailVerified() {
		return auth.Principal{}, false
	}
	mem, err := repo.GetMembership(ctx, sess.TenantID, sess.UserID)
	if err != nil {
		return auth.Principal{}, false
	}
	// Best-effort touch — ignore error, idle time is advisory.
	_ = repo.TouchSession(ctx, sess.ID, time.Now().UTC())
	return auth.Principal{
		ID:       user.ID,
		Email:    user.Email,
		TenantID: sess.TenantID,
		Roles:    mem.Roles,
		Groups:   mem.Groups,
	}, true
}
