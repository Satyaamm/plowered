package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/Satyaamm/plowered/internal/api/middleware"
	"github.com/Satyaamm/plowered/internal/core/auth"
	"github.com/Satyaamm/plowered/internal/core/email"
	"github.com/Satyaamm/plowered/internal/core/identity"
)

// AuthConfig is what the auth handlers need beyond the identity Repo +
// email Sender. WebBaseURL is the public origin where the verify link
// lands (e.g. https://app.plowered.io); FromAddress is the From: header
// on outbound transactional mail.
type AuthConfig struct {
	WebBaseURL  string
	FromAddress string
	CookieName  string
	CookieDomain string
	CookieSecure bool
	Logger      *slog.Logger
}

// AuthDeps groups the dependencies the handlers wrap. Created lazily in
// server.go and threaded into the mux via authHandlers().
type AuthDeps struct {
	Identity identity.Repo
	Email    email.Sender
	Config   AuthConfig
	Auth     middleware.AuthConfig // for bearer-token fallback during /me
}

// authHandlers registers the auth routes. They MUST live outside the
// authentication-required prefix list — signup, login, verify all run on
// unauthenticated requests.
func authHandlers(mux *http.ServeMux, d AuthDeps) {
	mux.HandleFunc("POST /v1/auth/signup",                signupHandler(d))
	mux.HandleFunc("POST /v1/auth/login",                 loginHandler(d))
	mux.HandleFunc("POST /v1/auth/logout",                logoutHandler(d))
	mux.HandleFunc("GET /v1/auth/verify",                 verifyHandler(d))
	mux.HandleFunc("POST /v1/auth/resend-verification",   resendVerificationHandler(d))
	mux.HandleFunc("GET /v1/auth/me",                     meHandler(d))
}

// ----- request / response shapes -----

type signupReq struct {
	Email           string `json:"email"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password,omitempty"` // optional; when set must match
	FullName        string `json:"full_name,omitempty"`        // legacy; FirstName+LastName preferred
	FirstName       string `json:"first_name,omitempty"`
	LastName        string `json:"last_name,omitempty"`
	Phone           string `json:"phone,omitempty"`         // subscriber digits
	PhoneCountry    string `json:"phone_country,omitempty"` // dial code, e.g. "+1"
	WorkspaceName   string `json:"workspace_name"`
	WorkspaceSlug   string `json:"workspace_slug"` // required; URL-safe identifier picked by the user
	AcceptTerms     bool   `json:"accept_terms,omitempty"`
}

type signupResp struct {
	TenantID string `json:"tenant_id"`
	UserID   string `json:"user_id"`
	Status   string `json:"status"`            // always "pending_verification"
	Message  string `json:"message"`
}

type loginReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginResp struct {
	UserID    string   `json:"user_id"`
	TenantID  string   `json:"tenant_id"`
	Email     string   `json:"email"`
	FullName  string   `json:"full_name"`
	Roles     []string `json:"roles"`
	ExpiresAt int64    `json:"expires_at"`
}

type meResp struct {
	UserID    string   `json:"user_id"`
	TenantID  string   `json:"tenant_id"`
	Email     string   `json:"email"`
	FullName  string   `json:"full_name"`
	Roles     []string `json:"roles"`
	Verified  bool     `json:"email_verified"`
}

// ----- handlers -----

func signupHandler(d AuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req signupReq
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if err := validateSignup(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"invalid_argument", err.Error()})
			return
		}
		ctx := r.Context()

		// 1. Hash password.
		hash, err := identity.HashPassword(req.Password)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody{"internal", "could not hash password"})
			return
		}

		// 2. Create tenant.
		tenant, err := d.Identity.CreateTenant(ctx, &identity.Tenant{
			Slug: req.WorkspaceSlug,
			Name: req.WorkspaceName,
		})
		if err != nil {
			if errors.Is(err, identity.ErrSlugTaken) {
				writeJSON(w, http.StatusConflict, errorBody{"slug_taken",
					"workspace slug already in use; pick another"})
				return
			}
			writeError(w, err)
			return
		}

		// 3. Create user (email_verified_at = NULL).
		fullName := strings.TrimSpace(req.FullName)
		if fullName == "" {
			fullName = strings.TrimSpace(req.FirstName + " " + req.LastName)
		}
		user, err := d.Identity.CreateUser(ctx, &identity.User{
			Email:        strings.TrimSpace(req.Email),
			FirstName:    strings.TrimSpace(req.FirstName),
			LastName:     strings.TrimSpace(req.LastName),
			FullName:     fullName,
			Phone:        strings.TrimSpace(req.Phone),
			PhoneCountry: strings.TrimSpace(req.PhoneCountry),
			PasswordHash: hash,
		})
		if err != nil {
			if errors.Is(err, identity.ErrEmailTaken) {
				writeJSON(w, http.StatusConflict, errorBody{"email_taken",
					"an account with this email already exists"})
				return
			}
			writeError(w, err)
			return
		}

		// 4. Membership: workspace creator gets admin + super_admin.
		if err := d.Identity.CreateMembership(ctx, &identity.Membership{
			TenantID:   tenant.ID,
			UserID:     user.ID,
			Roles:      []string{"super_admin", "admin"},
			InvitedBy:  user.ID,
			InvitedAt:  time.Now().UTC(),
			AcceptedAt: time.Now().UTC(),
		}); err != nil {
			writeError(w, err)
			return
		}

		// 5. Verification token + email.
		if err := sendVerification(ctx, d, user, tenant); err != nil {
			// Soft-fail: log and continue. The user can request a new link.
			if d.Config.Logger != nil {
				d.Config.Logger.Warn("signup: send verification failed",
					"user_id", user.ID, "err", err)
			}
		}

		writeJSON(w, http.StatusAccepted, signupResp{
			TenantID: tenant.ID,
			UserID:   user.ID,
			Status:   "pending_verification",
			Message:  "Check your email for a verification link to activate the workspace.",
		})
	}
}

func verifyHandler(d AuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(r.URL.Query().Get("token"))
		if token == "" {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "missing token"})
			return
		}
		ctx := r.Context()
		v, err := d.Identity.GetByToken(ctx, token)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"token_invalid", "verification link is invalid or already used"})
			return
		}
		if !v.UsedAt.IsZero() || time.Now().After(v.ExpiresAt) {
			writeJSON(w, http.StatusBadRequest, errorBody{"token_invalid", "verification link expired or already used"})
			return
		}
		if err := d.Identity.MarkEmailVerified(ctx, v.UserID, time.Now().UTC()); err != nil {
			writeError(w, err)
			return
		}
		_ = d.Identity.MarkUsed(ctx, v.ID, time.Now().UTC())
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "verified",
			"message": "Email verified — you can now sign in.",
		})
	}
}

func resendVerificationHandler(d AuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Email string `json:"email"` }
		if err := decodeJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		ctx := r.Context()
		u, err := d.Identity.GetByEmail(ctx, body.Email)
		if err != nil {
			// Don't leak whether the email is registered. Always respond 202.
			writeJSON(w, http.StatusAccepted, map[string]any{"status": "queued"})
			return
		}
		if u.IsEmailVerified() {
			writeJSON(w, http.StatusAccepted, map[string]any{"status": "already_verified"})
			return
		}
		// Find the user's first tenant for the email branding.
		mems, _ := d.Identity.ListForUser(ctx, u.ID)
		var tenant *identity.Tenant
		if len(mems) > 0 {
			tenant, _ = d.Identity.GetTenantByID(ctx, mems[0].TenantID)
		}
		_ = sendVerification(ctx, d, u, tenant)
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "queued"})
	}
}

func loginHandler(d AuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req loginReq
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		ctx := r.Context()
		req.Email = strings.ToLower(strings.TrimSpace(req.Email))
		u, err := d.Identity.GetByEmail(ctx, req.Email)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, errorBody{"invalid_credentials",
				"invalid email or password"})
			return
		}
		// Locked account refusal lives BEFORE the password check so the
		// lockout window can't be extended by spraying more attempts.
		if u.Status == "locked" {
			writeJSON(w, http.StatusForbidden, errorBody{"account_locked",
				"account locked after too many failed login attempts. Reset your password to unlock."})
			return
		}
		if err := identity.VerifyPassword(req.Password, u.PasswordHash); err != nil {
			// Bump the per-user failed-login counter. Lock when we hit
			// the threshold. The counter expires on its own after
			// FailedLoginWindow so an honest user with a typo today
			// isn't punished a week from now.
			n, _ := d.Identity.RecordFailedLogin(ctx, u.ID, time.Now().UTC())
			if n >= identity.FailedLoginThreshold {
				_ = d.Identity.LockUser(ctx, u.ID, "failed_login_threshold", time.Now().UTC())
			}
			writeJSON(w, http.StatusUnauthorized, errorBody{"invalid_credentials",
				"invalid email or password"})
			return
		}
		if !u.IsEmailVerified() {
			writeJSON(w, http.StatusForbidden, errorBody{"email_not_verified",
				"verify your email before signing in. Check your inbox or POST /v1/auth/resend-verification."})
			return
		}
		// Success path: clear the failed-login counter.
		_ = d.Identity.ResetFailedLogin(ctx, u.ID)

		// Pick first membership = active tenant.
		mems, err := d.Identity.ListForUser(ctx, u.ID)
		if err != nil || len(mems) == 0 {
			writeJSON(w, http.StatusForbidden, errorBody{"no_tenant",
				"user has no workspace; contact support"})
			return
		}
		mem := mems[0]

		// Mint session.
		sess, err := d.Identity.CreateSession(ctx, &identity.Session{
			UserID:    u.ID,
			TenantID:  mem.TenantID,
			IP:        clientIP(r),
			UserAgent: r.UserAgent(),
		})
		if err != nil {
			writeError(w, err)
			return
		}
		_ = d.Identity.UpdateLastLogin(ctx, u.ID, clientIP(r), time.Now().UTC())

		setSessionCookie(w, d.Config, sess)
		writeJSON(w, http.StatusOK, loginResp{
			UserID:    u.ID,
			TenantID:  mem.TenantID,
			Email:     u.Email,
			FullName:  u.FullName,
			Roles:     mem.Roles,
			ExpiresAt: sess.ExpiresAt.Unix(),
		})
	}
}

func logoutHandler(d AuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(d.Config.CookieName)
		if err == nil && c.Value != "" {
			_ = d.Identity.RevokeSession(r.Context(), c.Value, "logout", time.Now().UTC())
		}
		clearSessionCookie(w, d.Config)
		w.WriteHeader(http.StatusNoContent)
	}
}

func meHandler(d AuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// /me reads the principal the auth chain placed in context. If the
		// session was valid, AuthMW (session middleware) already set it.
		p, ok := principalFrom(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, errorBody{"unauthenticated",
				"no active session"})
			return
		}
		// Hydrate full user info from the repo so /me can power the
		// account dropdown without a separate /v1/users/me round-trip.
		u, err := d.Identity.GetUserByID(r.Context(), p.ID)
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, errorBody{"unauthenticated",
				"session valid but user missing"})
			return
		}
		writeJSON(w, http.StatusOK, meResp{
			UserID:   p.ID,
			TenantID: p.TenantID,
			Email:    u.Email,
			FullName: u.FullName,
			Roles:    p.Roles,
			Verified: u.IsEmailVerified(),
		})
	}
}

// ----- helpers -----

// emailRE is permissive on purpose — full RFC 5322 grammars are huge and
// most real validation happens later when the verification email either
// delivers or bounces. We block obviously-broken inputs.
var emailRE = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// phoneDigitsRE matches the subscriber digits only. Country code lives
// in PhoneCountry. We accept 6–15 digits (the upper bound is ITU-T
// E.164's 15 digits including country code, so this is conservative).
var phoneDigitsRE = regexp.MustCompile(`^\d{6,15}$`)

// dialCodeRE matches `+<digits>` (1–4 digits — covers every real
// country code).
var dialCodeRE = regexp.MustCompile(`^\+\d{1,4}$`)

// nameRE matches a permissive personal-name character class: letters
// (including non-ASCII Unicode), combining marks, spaces and a small
// punctuation set ('.-,). Digits and symbols are rejected so the field
// can't carry obvious garbage like "test1234".
var nameRE = regexp.MustCompile(`^[\p{L}\p{M}][\p{L}\p{M}\s'.,\-]*$`)

// workspaceSlugRE accepts lowercase letters, digits and dashes — must
// start and end with an alphanumeric. Picked by the user at signup;
// becomes the workspace's URL-safe identifier (e.g. `acme-data`).
var workspaceSlugRE = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)

func validateSignup(req *signupReq) error {
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	if req.Email == "" {
		return errors.New("email is required")
	}
	if len(req.Email) > 256 {
		return errors.New("email is too long")
	}
	if !emailRE.MatchString(req.Email) {
		return errors.New("email must be a valid email address")
	}
	if err := validatePasswordStrength(req.Password); err != nil {
		return err
	}
	if req.ConfirmPassword != "" && req.ConfirmPassword != req.Password {
		return errors.New("confirm_password does not match password")
	}
	req.WorkspaceName = strings.TrimSpace(req.WorkspaceName)
	if req.WorkspaceName == "" {
		return errors.New("workspace_name is required")
	}
	if len(req.WorkspaceName) < 2 {
		return errors.New("workspace_name must be at least 2 characters")
	}
	if len(req.WorkspaceName) > 64 {
		return errors.New("workspace_name exceeds 64 characters")
	}
	req.WorkspaceSlug = strings.ToLower(strings.TrimSpace(req.WorkspaceSlug))
	if req.WorkspaceSlug == "" {
		return errors.New("workspace_slug is required")
	}
	if len(req.WorkspaceSlug) < 2 {
		return errors.New("workspace_slug must be at least 2 characters")
	}
	if len(req.WorkspaceSlug) > 40 {
		return errors.New("workspace_slug exceeds 40 characters")
	}
	if !workspaceSlugRE.MatchString(req.WorkspaceSlug) {
		return errors.New("workspace_slug may only contain lowercase letters, digits and dashes, and may not start or end with a dash")
	}
	req.FirstName = strings.TrimSpace(req.FirstName)
	req.LastName = strings.TrimSpace(req.LastName)
	if req.FirstName != "" {
		if len(req.FirstName) > 64 {
			return errors.New("first_name exceeds 64 characters")
		}
		if !nameRE.MatchString(req.FirstName) {
			return errors.New("first_name must contain letters only")
		}
	}
	if req.LastName != "" {
		if len(req.LastName) > 64 {
			return errors.New("last_name exceeds 64 characters")
		}
		if !nameRE.MatchString(req.LastName) {
			return errors.New("last_name must contain letters only")
		}
	}
	if req.Phone != "" {
		// Strip user-friendly separators before validating.
		clean := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, req.Phone)
		if !phoneDigitsRE.MatchString(clean) {
			return errors.New("phone must be 6–15 digits")
		}
		req.Phone = clean
		if req.PhoneCountry == "" {
			return errors.New("phone_country is required when phone is set")
		}
		if !dialCodeRE.MatchString(req.PhoneCountry) {
			return errors.New(`phone_country must look like "+1" or "+91"`)
		}
	}
	return nil
}

// validatePasswordStrength enforces a hygienic minimum: 8+ chars, with
// at least three of the four classes (lower, upper, digit, special).
// Length and mix together resist dictionary attacks better than any
// single-class rule.
func validatePasswordStrength(p string) error {
	if len(p) < 8 {
		return errors.New("password must be at least 8 characters")
	}
	if len(p) > 256 {
		return errors.New("password too long")
	}
	var hasLower, hasUpper, hasDigit, hasSpecial bool
	for _, r := range p {
		switch {
		case r >= 'a' && r <= 'z':
			hasLower = true
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= '0' && r <= '9':
			hasDigit = true
		case r > 32 && r < 127:
			hasSpecial = true
		}
	}
	classes := 0
	for _, ok := range []bool{hasLower, hasUpper, hasDigit, hasSpecial} {
		if ok {
			classes++
		}
	}
	if classes < 3 {
		return errors.New("password must include three of: lowercase, uppercase, digit, special character")
	}
	return nil
}

func sendVerification(ctx context.Context, d AuthDeps, u *identity.User, t *identity.Tenant) error {
	if d.Email == nil {
		return errors.New("email sender not configured")
	}
	tok, err := identity.NewToken()
	if err != nil {
		return err
	}
	if err := d.Identity.CreateVerification(ctx, &identity.Verification{
		UserID:    u.ID,
		Token:     tok,
		Purpose:   identity.PurposeVerifyEmail,
		ExpiresAt: time.Now().UTC().Add(identity.VerificationTTL),
	}); err != nil {
		return err
	}
	verifyURL := buildVerifyURL(d.Config.WebBaseURL, tok)
	wsName := ""
	if t != nil {
		wsName = t.Name
	}
	msg := email.VerificationTemplate(wsName, u.Email, verifyURL)
	if msg.From == "" {
		msg.From = d.Config.FromAddress
	}
	return d.Email.Send(ctx, msg)
}

func buildVerifyURL(base, token string) string {
	if base == "" {
		base = "http://localhost:3000"
	}
	q := url.Values{}
	q.Set("token", token)
	return strings.TrimRight(base, "/") + "/verify?" + q.Encode()
}

func setSessionCookie(w http.ResponseWriter, cfg AuthConfig, s *identity.Session) {
	name := cfg.CookieName
	if name == "" {
		name = "plowered_session"
	}
	c := &http.Cookie{
		Name:     name,
		Value:    s.ID,
		Path:     "/",
		Expires:  s.ExpiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   cfg.CookieSecure,
	}
	if cfg.CookieDomain != "" {
		c.Domain = cfg.CookieDomain
	}
	http.SetCookie(w, c)
}

func clearSessionCookie(w http.ResponseWriter, cfg AuthConfig) {
	name := cfg.CookieName
	if name == "" {
		name = "plowered_session"
	}
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   cfg.CookieSecure,
	})
}

// clientIP lives in audit_mw.go; reuse it.

// _ keeps auth.Principal referenced for clarity even when none of the
// returns directly export it; principalFrom returns the typed struct.
var _ = auth.Principal{}
