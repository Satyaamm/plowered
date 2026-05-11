package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Satyaamm/plowered/internal/core/identity"
)

// GDPR self-service endpoints. Article 20 (data portability) and
// Article 17 (right to erasure) are the two rights a tenant member can
// exercise on themselves without an admin in the loop.
//
//	GET    /v1/account/export   bundle every personal-data row we hold
//	DELETE /v1/account          pseudonymise the user + kill sessions
//
// "Pseudonymise" instead of "DELETE FROM users" because:
//   - The user authored assets (created_by FKs) we cannot null out
//     without losing change-history.
//   - GDPR Art. 17(3) allows retention for "compliance with a legal
//     obligation" — our audit log is exactly that under SOC2 + HIPAA.
//   - Tenants want to keep "Olivia Owner made this change in 2024"
//     visible to investigators even after Olivia leaves.
//
// What we do: overwrite email + names + phone with deterministic
// placeholders, flip status='deleted', revoke every session and
// outstanding token. The audit log keeps the user_id; nothing else.
//
// Admin-driven DSR (the GDPR-ticketing surface for "the regulator
// asked us to delete this customer") lives in /v1/dsr/* and uses the
// DSR repo. This file is the self-service path only.
func accountGDPRHandlers(mux *http.ServeMux, d AuthDeps) {
	mux.HandleFunc("GET /v1/account/export", exportAccountHandler(d))
	mux.HandleFunc("DELETE /v1/account", deleteAccountHandler(d))
}

func exportAccountHandler(d AuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pr, ok := principalFrom(r)
		if !ok || pr.ID == "" {
			writeJSON(w, http.StatusUnauthorized, errorBody{"unauthorized", "authentication required"})
			return
		}
		ctx := r.Context()
		user, err := d.Identity.GetUserByID(ctx, pr.ID)
		if err != nil {
			writeError(w, err)
			return
		}
		memberships, _ := d.Identity.ListForUser(ctx, pr.ID)
		sessions, _ := d.Identity.ListActiveSessionsForUser(ctx, pr.ID)

		// The shape is intentionally a flat JSON bundle so a regulator
		// or end-user can `jq` over it without learning our schema.
		// `_meta.generated_at` + `_meta.format_version` let us evolve
		// without breaking downstream parsers.
		bundle := map[string]any{
			"_meta": map[string]any{
				"format_version": 1,
				"generated_at":   time.Now().UTC().Format(time.RFC3339),
				"user_id":        user.ID,
				"description":    "Plowered GDPR Art. 20 data export — every personal-data row tied to this user.",
			},
			"profile": map[string]any{
				"id":                user.ID,
				"email":             user.Email,
				"first_name":        user.FirstName,
				"last_name":         user.LastName,
				"full_name":         user.FullName,
				"phone":             user.Phone,
				"phone_country":     user.PhoneCountry,
				"avatar_url":        user.AvatarURL,
				"status":            user.Status,
				"email_verified_at": jsonTime(user.EmailVerifiedAt),
				"last_login_at":     jsonTime(user.LastLoginAt),
				"last_login_ip":     user.LastLoginIP,
				"created_at":        jsonTime(user.CreatedAt),
				"updated_at":        jsonTime(user.UpdatedAt),
			},
			"memberships": exportMemberships(memberships),
			"sessions":    exportSessions(sessions),
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition",
			fmt.Sprintf(`attachment; filename="plowered-export-%s.json"`, user.ID))
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(bundle)
	}
}

func deleteAccountHandler(d AuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pr, ok := principalFrom(r)
		if !ok || pr.ID == "" {
			writeJSON(w, http.StatusUnauthorized, errorBody{"unauthorized", "authentication required"})
			return
		}
		// Soft-confirm via a query flag to make this hard to hit by
		// accident. The UI sends ?confirm=true after a typed-confirmation
		// dialog. Without it we 400.
		if r.URL.Query().Get("confirm") != "true" {
			writeJSON(w, http.StatusBadRequest, errorBody{"confirm_required",
				"add ?confirm=true to acknowledge that this action is irreversible"})
			return
		}
		ctx := r.Context()
		now := time.Now().UTC()
		// Pseudonymise — email becomes "deleted-<id>@deleted.invalid",
		// names and phone get wiped. The audit log keeps the user_id so
		// historical actions remain investigable; what's gone is the
		// PII that links it to a real person.
		stub := "deleted-" + strings.ReplaceAll(pr.ID, "-", "") + "@deleted.invalid"
		if err := d.Identity.PseudonymiseUser(ctx, pr.ID, stub, now); err != nil {
			if errors.Is(err, identity.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, errorBody{"not_found", "user not found"})
				return
			}
			writeError(w, err)
			return
		}
		_ = d.Identity.RevokeAllSessionsForUser(ctx, pr.ID, "account_deleted", now)
		clearSessionCookie(w, d.Config)
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "deleted",
			"message": "Account pseudonymised. Sessions revoked. Audit history retained per legal-obligation exemption (GDPR Art. 17(3)(b)).",
		})
	}
}

func exportMemberships(ms []*identity.Membership) []map[string]any {
	out := make([]map[string]any, 0, len(ms))
	for _, m := range ms {
		out = append(out, map[string]any{
			"tenant_id":   m.TenantID,
			"roles":       m.Roles,
			"invited_by":  m.InvitedBy,
			"invited_at":  jsonTime(m.InvitedAt),
			"accepted_at": jsonTime(m.AcceptedAt),
		})
	}
	return out
}

func exportSessions(ss []*identity.Session) []map[string]any {
	out := make([]map[string]any, 0, len(ss))
	for _, s := range ss {
		out = append(out, map[string]any{
			"id":           s.ID,
			"ip":           s.IP,
			"user_agent":   s.UserAgent,
			"issued_at":    jsonTime(s.IssuedAt),
			"last_seen_at": jsonTime(s.LastSeenAt),
			"expires_at":   jsonTime(s.ExpiresAt),
		})
	}
	return out
}

// jsonTime renders zero-valued times as null (cleaner than the Go
// default "0001-01-01T00:00:00Z") so the export is friendlier to grep.
func jsonTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.Format(time.RFC3339)
}
