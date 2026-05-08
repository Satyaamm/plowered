package http

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/Satyaamm/plowered/internal/api/middleware"
	"github.com/Satyaamm/plowered/internal/core/auth"
)

// SignupRequest is the JSON body for POST /v1/signup.
type SignupRequest struct {
	Email      string `json:"email"`
	TenantName string `json:"tenant_name"`
}

// SignupResponse is the body returned to a successful sign-up.
type SignupResponse struct {
	TenantID  string `json:"tenant_id"`
	UserID    string `json:"user_id"`
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expires_at"`
}

// SignupConfig is the dependency the signup handler needs to mint a JWT.
// Pass the same AuthConfig the rest of the server uses.
//
// v0 cloud-preview limitations:
//   - Tenant rows live only in JWT claims; no tenants table yet.
//   - No password — magic-link / OIDC follow.
//   - Token TTL fixed at 1h; refresh flow is a follow-up.
type SignupConfig struct {
	AuthConfig middleware.AuthConfig
	TokenTTL   time.Duration
}

// SignupHandler returns POST /v1/signup. It is intentionally registered
// outside the auth-required prefix list since the whole point is letting
// unauthenticated callers create their first tenant.
func SignupHandler(cfg SignupConfig) http.HandlerFunc {
	if cfg.TokenTTL == 0 {
		cfg.TokenTTL = time.Hour
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var req SignupRequest
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if err := validateSignup(req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"invalid_argument", err.Error()})
			return
		}
		if cfg.AuthConfig.HS256Secret == nil && cfg.AuthConfig.RS256PublicKey == nil {
			writeJSON(w, http.StatusServiceUnavailable, errorBody{
				"unconfigured",
				"signup requires JWT signing keys; see PLOWERED_JWT_HS256_SECRET in .env.example",
			})
			return
		}

		tenantID := newID("tnt_")
		userID := newID("usr_")
		token, exp, err := signTenantToken(cfg, auth.Principal{
			ID:       userID,
			Email:    req.Email,
			TenantID: tenantID,
			Roles:    []string{"admin"},
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody{"internal", err.Error()})
			return
		}

		writeJSON(w, http.StatusCreated, SignupResponse{
			TenantID:  tenantID,
			UserID:    userID,
			Token:     token,
			ExpiresAt: exp.Unix(),
		})
	}
}

func validateSignup(req SignupRequest) error {
	if !strings.Contains(req.Email, "@") {
		return errors.New("email is required and must contain @")
	}
	if len(req.Email) > 254 {
		return errors.New("email exceeds 254 chars")
	}
	if req.TenantName != "" && len(req.TenantName) > 64 {
		return errors.New("tenant_name exceeds 64 chars")
	}
	return nil
}

func signTenantToken(cfg SignupConfig, p auth.Principal) (string, time.Time, error) {
	exp := time.Now().Add(cfg.TokenTTL)
	claims := jwt.MapClaims{
		"sub":   p.ID,
		"email": p.Email,
		"tid":   p.TenantID,
		"roles": p.Roles,
		"iss":   firstNonEmpty(cfg.AuthConfig.Issuer, "plowered"),
		"aud":   firstNonEmpty(cfg.AuthConfig.Audience, "plowered"),
		"exp":   exp.Unix(),
		"iat":   time.Now().Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	if cfg.AuthConfig.HS256Secret == nil {
		return "", time.Time{}, errors.New("HS256 secret required to sign cloud-preview tokens; RS256 signing not yet wired")
	}
	signed, err := tok.SignedString(cfg.AuthConfig.HS256Secret)
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, exp, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func newID(prefix string) string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}
