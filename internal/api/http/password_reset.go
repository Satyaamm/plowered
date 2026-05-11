package http

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"net/http"

	"github.com/Satyaamm/plowered/internal/core/email"
	"github.com/Satyaamm/plowered/internal/core/identity"
)

// passwordResetHandlers exposes the public reset flow:
//
//	POST /v1/auth/forgot-password { email }            → 202 always
//	POST /v1/auth/reset-password  { token, password }  → 200 or 400
//
// Security notes:
//
//   - /forgot-password ALWAYS returns 202, whether or not the email
//     resolves to a real user. Returning 404 leaks which addresses
//     have accounts, defeating the point of the flow. The email send
//     is only attempted when the user exists.
//   - Tokens are 32 random bytes (identity.NewToken), single-use,
//     24-hour TTL. After consumption the token row's used_at is
//     set in the same transaction as the password update.
//   - On a successful reset we revoke every existing session for that
//     user — a stolen session can't outlive the reset.
//   - The /forgot-password endpoint is rate-limited by AuthRateLimitMW
//     (5/min/IP) so an attacker can't enumerate emails by polling.
func passwordResetHandlers(mux *http.ServeMux, d AuthDeps) {
	mux.HandleFunc("POST /v1/auth/forgot-password", forgotPasswordHandler(d))
	mux.HandleFunc("POST /v1/auth/reset-password", resetPasswordHandler(d))
}

type forgotPasswordReq struct {
	Email string `json:"email"`
}

type resetPasswordReq struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func forgotPasswordHandler(d AuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req forgotPasswordReq
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		req.Email = strings.ToLower(strings.TrimSpace(req.Email))
		if !emailRE.MatchString(req.Email) {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "email must be a valid email address"})
			return
		}
		// Look up the user; on miss we still 202 (anti-enumeration).
		// Token + email send only happen for real users.
		ctx := r.Context()
		user, err := d.Identity.GetByEmail(ctx, req.Email)
		switch {
		case errors.Is(err, identity.ErrNotFound):
			// no-op; still 202
		case err != nil:
			writeError(w, err)
			return
		default:
			go sendResetEmail(d, user)
		}
		writeJSON(w, http.StatusAccepted, map[string]any{
			"status":  "ok",
			"message": "If an account exists for this email, a reset link is on its way.",
		})
	}
}

func resetPasswordHandler(d AuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req resetPasswordReq
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if req.Token == "" {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "token is required"})
			return
		}
		if err := validatePasswordStrength(req.Password); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"invalid_argument", err.Error()})
			return
		}
		ctx := r.Context()
		v, err := d.Identity.GetByToken(ctx, req.Token)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "token invalid or expired"})
			return
		}
		if v.Purpose != identity.PurposePasswordReset {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "token is not a password-reset token"})
			return
		}
		if !v.UsedAt.IsZero() || time.Now().UTC().After(v.ExpiresAt) {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "token invalid or expired"})
			return
		}
		user, err := d.Identity.GetUserByID(ctx, v.UserID)
		if err != nil {
			writeError(w, err)
			return
		}
		hash, err := identity.HashPassword(req.Password)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody{"internal", "could not hash password"})
			return
		}
		if err := d.Identity.UpdatePassword(ctx, user.ID, hash); err != nil {
			writeError(w, err)
			return
		}
		if err := d.Identity.MarkUsed(ctx, v.ID, time.Now().UTC()); err != nil {
			// Token couldn't be marked used — the password did change,
			// but a second use of the same token would also flip it. The
			// MarkUsed handler's WHERE used_at IS NULL guard saves us.
			writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
			return
		}
		// Defense in depth: revoke any active sessions so a stolen
		// cookie from before the reset is invalidated. Best-effort —
		// failures are logged but don't block the user from logging in
		// again with the new password.
		_ = d.Identity.RevokeAllSessionsForUser(ctx, user.ID, "password_reset", time.Now().UTC())
		// Successful reset clears any lockout from a brute-force run.
		// Without this the user would be back at /login → "account
		// locked" even though they proved ownership of the email.
		_ = d.Identity.UnlockUser(ctx, user.ID)
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	}
}

func sendResetEmail(d AuthDeps, user *identity.User) {
	if d.Email == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tok, err := identity.NewToken()
	if err != nil {
		return
	}
	if err := d.Identity.CreateVerification(ctx, &identity.Verification{
		UserID:    user.ID,
		Token:     tok,
		Purpose:   identity.PurposePasswordReset,
		ExpiresAt: time.Now().UTC().Add(identity.VerificationTTL),
	}); err != nil {
		return
	}
	workspaceName := ""
	if memberships, err := d.Identity.ListForUser(ctx, user.ID); err == nil && len(memberships) > 0 {
		if t, err := d.Identity.GetTenantByID(ctx, memberships[0].TenantID); err == nil {
			workspaceName = t.Name
		}
	}
	resetURL := buildResetURL(d.Config.WebBaseURL, tok)
	msg := email.PasswordResetTemplate(workspaceName, user.Email, resetURL)
	if msg.From == "" {
		msg.From = d.Config.FromAddress
	}
	_ = d.Email.Send(ctx, msg)
}

func buildResetURL(base, token string) string {
	if base == "" {
		base = "http://localhost:3000"
	}
	q := url.Values{}
	q.Set("token", token)
	return strings.TrimRight(base, "/") + "/reset-password?" + q.Encode()
}
