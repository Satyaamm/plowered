package http

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Satyaamm/plowered/internal/core/email"
	"github.com/Satyaamm/plowered/internal/core/identity"
)

// teamHandlers exposes the team-management surface on top of AuthDeps:
//
//	GET    /v1/members             list members of caller's tenant
//	PATCH  /v1/members/{user_id}   change roles (admin only)
//	DELETE /v1/members/{user_id}   remove member (admin only)
//
//	GET    /v1/invites             list pending invites (?include=all)
//	POST   /v1/invites             send a new invite (admin only)
//	DELETE /v1/invites/{id}        revoke (admin only)
//	POST   /v1/auth/accept-invite  public; creates user + membership
//	GET    /v1/auth/invite-info    public; preview invite by token
//
// Admin gate: the auth chain already populates principal.Roles; we
// require role "admin" for any write-side operation. A future RBAC
// pass can move this onto the policy.Engine.
func teamHandlers(mux *http.ServeMux, d AuthDeps) {
	mux.HandleFunc("GET /v1/members", listMembersHandler(d))
	mux.HandleFunc("PATCH /v1/members/{user_id}", updateMemberHandler(d))
	mux.HandleFunc("DELETE /v1/members/{user_id}", removeMemberHandler(d))

	mux.HandleFunc("GET /v1/invites", listInvitesHandler(d))
	mux.HandleFunc("POST /v1/invites", createInviteHandler(d))
	mux.HandleFunc("DELETE /v1/invites/{id}", revokeInviteHandler(d))

	mux.HandleFunc("GET /v1/auth/invite-info", inviteInfoHandler(d))
	mux.HandleFunc("POST /v1/auth/accept-invite", acceptInviteHandler(d))
}

// ----- shapes -----

type memberResp struct {
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	FullName  string    `json:"full_name"`
	FirstName string    `json:"first_name,omitempty"`
	LastName  string    `json:"last_name,omitempty"`
	Roles     []string  `json:"roles"`
	Status    string    `json:"status"`
	InvitedAt time.Time `json:"invited_at"`
	JoinedAt  time.Time `json:"joined_at,omitempty"`
}

type inviteResp struct {
	ID         string    `json:"id"`
	Email      string    `json:"email"`
	Roles      []string  `json:"roles"`
	Status     string    `json:"status"` // pending | accepted | revoked | expired
	InvitedBy  string    `json:"invited_by,omitempty"`
	ExpiresAt  time.Time `json:"expires_at"`
	CreatedAt  time.Time `json:"created_at"`
	AcceptedAt time.Time `json:"accepted_at,omitempty"`
	RevokedAt  time.Time `json:"revoked_at,omitempty"`
}

func inviteStatus(i *identity.Invite, now time.Time) string {
	switch {
	case !i.RevokedAt.IsZero():
		return "revoked"
	case !i.AcceptedAt.IsZero():
		return "accepted"
	case now.After(i.ExpiresAt):
		return "expired"
	default:
		return "pending"
	}
}

type createInviteReq struct {
	Email string   `json:"email"`
	Roles []string `json:"roles"`
}

type updateMemberReq struct {
	Roles []string `json:"roles"`
}

type acceptInviteReq struct {
	Token     string `json:"token"`
	Password  string `json:"password"`
	FirstName string `json:"first_name,omitempty"`
	LastName  string `json:"last_name,omitempty"`
}

// ----- helpers -----

func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	pr, ok := principalFrom(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, errorBody{"unauthorized", "authentication required"})
		return false
	}
	for _, role := range pr.Roles {
		if role == "admin" || role == "super_admin" {
			return true
		}
	}
	writeJSON(w, http.StatusForbidden, errorBody{"forbidden", "admin role required"})
	return false
}

func validRoles(roles []string) []string {
	allowed := map[string]bool{
		"viewer": true, "editor": true, "steward": true, "admin": true,
	}
	out := []string{}
	seen := map[string]bool{}
	for _, r := range roles {
		r = strings.ToLower(strings.TrimSpace(r))
		if allowed[r] && !seen[r] {
			out = append(out, r)
			seen[r] = true
		}
	}
	if len(out) == 0 {
		out = []string{"viewer"}
	}
	return out
}

// ----- members -----

func listMembersHandler(d AuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		members, err := d.Identity.ListMembershipsForTenant(r.Context(), tenant)
		if err != nil {
			writeError(w, err)
			return
		}
		out := make([]memberResp, 0, len(members))
		for _, m := range members {
			out = append(out, memberResp{
				UserID:    m.UserID,
				Email:     m.Email,
				FullName:  m.FullName,
				FirstName: m.FirstName,
				LastName:  m.LastName,
				Roles:     m.Roles,
				Status:    m.Status,
				InvitedAt: m.InvitedAt,
				JoinedAt:  m.AcceptedAt,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"members": out})
	}
}

func updateMemberHandler(d AuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" || !requireAdmin(w, r) {
			return
		}
		userID := r.PathValue("user_id")
		var req updateMemberReq
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if err := d.Identity.UpdateMembershipRoles(r.Context(), tenant, userID, validRoles(req.Roles)); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	}
}

func removeMemberHandler(d AuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" || !requireAdmin(w, r) {
			return
		}
		userID := r.PathValue("user_id")
		// Self-removal guard: an admin removing themselves would lock
		// the tenant out. The first version of this UI also forbids
		// removing the last admin entirely.
		if pr, _ := principalFrom(r); pr.ID == userID {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "use account settings to leave your workspace"})
			return
		}
		if err := d.Identity.DeleteMembership(r.Context(), tenant, userID); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ----- invites -----

func listInvitesHandler(d AuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		includeAll := r.URL.Query().Get("include") == "all"
		invites, err := d.Identity.ListInvitesForTenant(r.Context(), tenant, !includeAll)
		if err != nil {
			writeError(w, err)
			return
		}
		now := time.Now().UTC()
		out := make([]inviteResp, 0, len(invites))
		for _, i := range invites {
			out = append(out, inviteResp{
				ID:         i.ID,
				Email:      i.Email,
				Roles:      i.Roles,
				Status:     inviteStatus(i, now),
				InvitedBy:  i.InvitedBy,
				ExpiresAt:  i.ExpiresAt,
				CreatedAt:  i.CreatedAt,
				AcceptedAt: i.AcceptedAt,
				RevokedAt:  i.RevokedAt,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"invites": out})
	}
}

func createInviteHandler(d AuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" || !requireAdmin(w, r) {
			return
		}
		var req createInviteReq
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		req.Email = strings.ToLower(strings.TrimSpace(req.Email))
		if !emailRE.MatchString(req.Email) {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "email is required and must be valid"})
			return
		}
		// Prevent inviting an existing member.
		if existing, err := d.Identity.GetByEmail(r.Context(), req.Email); err == nil {
			if _, err := d.Identity.GetMembership(r.Context(), tenant, existing.ID); err == nil {
				writeJSON(w, http.StatusConflict, errorBody{"conflict", "this user is already a member of the workspace"})
				return
			}
		}
		// Coalesce duplicate pending invites — revoke any that exist
		// for the same (tenant, email) so the new one becomes the
		// authoritative outstanding invite.
		if existing, err := d.Identity.ListInvitesForTenant(r.Context(), tenant, true); err == nil {
			now := time.Now().UTC()
			for _, ex := range existing {
				if strings.EqualFold(ex.Email, req.Email) {
					_ = d.Identity.RevokeInvite(r.Context(), tenant, ex.ID, now)
				}
			}
		}
		tok, err := identity.NewToken()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody{"internal", "could not mint token"})
			return
		}
		actor := ""
		if pr, ok := principalFrom(r); ok {
			actor = pr.ID
		}
		inv := &identity.Invite{
			TenantID:  tenant,
			Email:     req.Email,
			Roles:     validRoles(req.Roles),
			Token:     tok,
			InvitedBy: actor,
			ExpiresAt: time.Now().UTC().Add(identity.InviteTTL),
		}
		if err := d.Identity.CreateInvite(r.Context(), inv); err != nil {
			writeError(w, err)
			return
		}
		go sendInvite(d, tenant, req.Email, tok, actor)
		writeJSON(w, http.StatusCreated, inviteResp{
			ID:        inv.ID,
			Email:     inv.Email,
			Roles:     inv.Roles,
			Status:    "pending",
			InvitedBy: inv.InvitedBy,
			ExpiresAt: inv.ExpiresAt,
			CreatedAt: inv.CreatedAt,
		})
	}
}

func revokeInviteHandler(d AuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" || !requireAdmin(w, r) {
			return
		}
		if err := d.Identity.RevokeInvite(r.Context(), tenant, r.PathValue("id"), time.Now().UTC()); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// inviteInfoHandler is the public preview the /accept-invite page hits
// before the user commits a password. Reveals only the email + workspace
// name — never the tenant ID or any other invite-listing metadata.
func inviteInfoHandler(d AuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := r.URL.Query().Get("token")
		if tok == "" {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "token is required"})
			return
		}
		inv, err := d.Identity.GetInviteByToken(r.Context(), tok)
		if err != nil {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", "invite not found"})
			return
		}
		if !inv.IsPending(time.Now().UTC()) {
			writeJSON(w, http.StatusGone, errorBody{"gone", "invite no longer valid"})
			return
		}
		tenant, err := d.Identity.GetTenantByID(r.Context(), inv.TenantID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"email":          inv.Email,
			"workspace_name": tenant.Name,
			"roles":          inv.Roles,
		})
	}
}

func acceptInviteHandler(d AuthDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req acceptInviteReq
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
		inv, err := d.Identity.GetInviteByToken(ctx, req.Token)
		if err != nil {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", "invite not found"})
			return
		}
		if !inv.IsPending(time.Now().UTC()) {
			writeJSON(w, http.StatusGone, errorBody{"gone", "invite no longer valid"})
			return
		}
		// Resolve / create the user. If the email already has an
		// account in another tenant, attach a membership instead.
		user, err := d.Identity.GetByEmail(ctx, inv.Email)
		switch {
		case err == nil:
			// Existing user — leave password alone, just attach.
		case errors.Is(err, identity.ErrNotFound):
			hash, hErr := identity.HashPassword(req.Password)
			if hErr != nil {
				writeJSON(w, http.StatusInternalServerError, errorBody{"internal", "could not hash password"})
				return
			}
			now := time.Now().UTC()
			fullName := strings.TrimSpace(req.FirstName + " " + req.LastName)
			created, cErr := d.Identity.CreateUser(ctx, &identity.User{
				Email:           inv.Email,
				FirstName:       req.FirstName,
				LastName:        req.LastName,
				FullName:        fullName,
				PasswordHash:    hash,
				Status:          "active",
				EmailVerifiedAt: now, // accepting the email = proof of ownership
			})
			if cErr != nil {
				writeError(w, cErr)
				return
			}
			user = created
		default:
			writeError(w, err)
			return
		}
		// Idempotent: an already-attached membership is fine.
		_ = d.Identity.CreateMembership(ctx, &identity.Membership{
			TenantID:   inv.TenantID,
			UserID:     user.ID,
			Roles:      inv.Roles,
			InvitedBy:  inv.InvitedBy,
			AcceptedAt: time.Now().UTC(),
		})
		if err := d.Identity.MarkInviteAccepted(ctx, inv.ID, time.Now().UTC()); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":    "joined",
			"tenant_id": inv.TenantID,
			"user_id":   user.ID,
		})
	}
}

// sendInvite fires the invitation email best-effort. Errors are logged
// (in real deployments via slog wired to the email sender) but never
// surface — the admin already saw a 201 with the invite row. The web
// app's invites list shows "resend" if the user reports they never got
// it; resending re-creates an invite, which generates a fresh token.
func sendInvite(d AuthDeps, tenantID, recipient, token, inviterID string) {
	if d.Email == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	t, err := d.Identity.GetTenantByID(ctx, tenantID)
	if err != nil || t == nil {
		return
	}
	inviterEmail := ""
	if inviterID != "" {
		if u, err := d.Identity.GetUserByID(ctx, inviterID); err == nil {
			inviterEmail = u.Email
		}
	}
	url := buildInviteURL(d.Config.WebBaseURL, token)
	msg := email.InvitationTemplate(t.Name, inviterEmail, recipient, url)
	if msg.From == "" {
		msg.From = d.Config.FromAddress
	}
	_ = d.Email.Send(ctx, msg)
}

func buildInviteURL(base, token string) string {
	if base == "" {
		base = "http://localhost:3000"
	}
	q := url.Values{}
	q.Set("token", token)
	return strings.TrimRight(base, "/") + "/accept-invite?" + q.Encode()
}
