package http

import (
	"net/http"
	"strings"
	"time"

	"github.com/Satyaamm/plowered/internal/core/identity"
)

// accountHandlers exposes the self-service account-settings surface:
//
//	PATCH  /v1/account/profile         change first/last/phone
//	POST   /v1/account/change-password current + new (rejects on mismatch)
//	GET    /v1/account/sessions        list active sessions ("devices")
//	DELETE /v1/account/sessions        sign out everywhere (kills all)
//	DELETE /v1/account/sessions/{id}   revoke one session (single device)
//
// Every endpoint requires an authenticated session — the auth chain
// runs first, principalFrom() pulls the user ID from context. We never
// trust a body-supplied user_id; the caller can only mutate themselves.
func accountHandlers(mux *http.ServeMux, d AuthDeps) {
	mux.HandleFunc("PATCH /v1/account/profile", updateProfileHandler(d))
	mux.HandleFunc("POST /v1/account/change-password", changePasswordHandler(d))
	mux.HandleFunc("GET /v1/account/sessions", listSessionsHandler(d))
	mux.HandleFunc("DELETE /v1/account/sessions", revokeAllSessionsHandler(d))
	mux.HandleFunc("DELETE /v1/account/sessions/{id}", revokeOneSessionHandler(d))
}

type updateProfileReq struct {
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	Phone        string `json:"phone"`
	PhoneCountry string `json:"phone_country"`
}

type changePasswordReq struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type sessionResp struct {
	ID         string    `json:"id"`
	IP         string    `json:"ip,omitempty"`
	UserAgent  string    `json:"user_agent,omitempty"`
	IssuedAt   time.Time `json:"issued_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	Current    bool      `json:"current"`
}

func updateProfileHandler(d AuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pr, ok := principalFrom(r)
		if !ok || pr.ID == "" {
			writeJSON(w, http.StatusUnauthorized, errorBody{"unauthorized", "authentication required"})
			return
		}
		var req updateProfileReq
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		req.FirstName = strings.TrimSpace(req.FirstName)
		req.LastName = strings.TrimSpace(req.LastName)
		if req.FirstName != "" && (!nameRE.MatchString(req.FirstName) || len(req.FirstName) > 64) {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "first_name must be letters only, max 64 chars"})
			return
		}
		if req.LastName != "" && (!nameRE.MatchString(req.LastName) || len(req.LastName) > 64) {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "last_name must be letters only, max 64 chars"})
			return
		}
		phone := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, req.Phone)
		if phone != "" && !phoneDigitsRE.MatchString(phone) {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "phone must be 6–15 digits"})
			return
		}
		if phone != "" && req.PhoneCountry != "" && !dialCodeRE.MatchString(req.PhoneCountry) {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", `phone_country must look like "+1" or "+91"`})
			return
		}
		err := d.Identity.UpdateProfile(r.Context(), pr.ID, identity.ProfileUpdate{
			FirstName:    req.FirstName,
			LastName:     req.LastName,
			Phone:        phone,
			PhoneCountry: req.PhoneCountry,
		})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	}
}

func changePasswordHandler(d AuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pr, ok := principalFrom(r)
		if !ok || pr.ID == "" {
			writeJSON(w, http.StatusUnauthorized, errorBody{"unauthorized", "authentication required"})
			return
		}
		var req changePasswordReq
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if req.CurrentPassword == "" {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "current_password is required"})
			return
		}
		if err := validatePasswordStrength(req.NewPassword); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"invalid_argument", err.Error()})
			return
		}
		ctx := r.Context()
		user, err := d.Identity.GetUserByID(ctx, pr.ID)
		if err != nil {
			writeError(w, err)
			return
		}
		if err := identity.VerifyPassword(req.CurrentPassword, user.PasswordHash); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"invalid_argument", "current password is incorrect"})
			return
		}
		hash, err := identity.HashPassword(req.NewPassword)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody{"internal", "could not hash password"})
			return
		}
		if err := d.Identity.UpdatePassword(ctx, pr.ID, hash); err != nil {
			writeError(w, err)
			return
		}
		// Kill any other sessions so a stolen cookie from before the
		// rotation can't outlive the new password. The current session
		// stays alive — we'd otherwise log the user out mid-request.
		// (Implementation note: revoking all-except-current would need
		// the session ID off the cookie context; for v0 we revoke all
		// and accept that the user must re-login.)
		_ = d.Identity.RevokeAllSessionsForUser(ctx, pr.ID, "password_changed", time.Now().UTC())
		clearSessionCookie(w, d.Config)
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "ok",
			"message": "Password changed. Sign in again with the new password.",
		})
	}
}

func listSessionsHandler(d AuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pr, ok := principalFrom(r)
		if !ok || pr.ID == "" {
			writeJSON(w, http.StatusUnauthorized, errorBody{"unauthorized", "authentication required"})
			return
		}
		sessions, err := d.Identity.ListActiveSessionsForUser(r.Context(), pr.ID)
		if err != nil {
			writeError(w, err)
			return
		}
		currentID := ""
		if c, err := r.Cookie(d.Config.CookieName); err == nil {
			currentID = c.Value
		}
		out := make([]sessionResp, 0, len(sessions))
		for _, s := range sessions {
			out = append(out, sessionResp{
				ID:         s.ID,
				IP:         s.IP,
				UserAgent:  s.UserAgent,
				IssuedAt:   s.IssuedAt,
				LastSeenAt: s.LastSeenAt,
				ExpiresAt:  s.ExpiresAt,
				Current:    s.ID == currentID,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"sessions": out})
	}
}

func revokeAllSessionsHandler(d AuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pr, ok := principalFrom(r)
		if !ok || pr.ID == "" {
			writeJSON(w, http.StatusUnauthorized, errorBody{"unauthorized", "authentication required"})
			return
		}
		if err := d.Identity.RevokeAllSessionsForUser(r.Context(), pr.ID, "user_signout_all", time.Now().UTC()); err != nil {
			writeError(w, err)
			return
		}
		clearSessionCookie(w, d.Config)
		w.WriteHeader(http.StatusNoContent)
	}
}

func revokeOneSessionHandler(d AuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pr, ok := principalFrom(r)
		if !ok || pr.ID == "" {
			writeJSON(w, http.StatusUnauthorized, errorBody{"unauthorized", "authentication required"})
			return
		}
		id := r.PathValue("id")
		// Confirm the session belongs to this user — otherwise an
		// attacker with one cookie could revoke another tenant's
		// sessions by guessing their UUIDs.
		sess, err := d.Identity.GetSession(r.Context(), id)
		if err != nil || sess.UserID != pr.ID {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", "session not found"})
			return
		}
		if err := d.Identity.RevokeSession(r.Context(), id, "user_revoked", time.Now().UTC()); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

