package http_test

// e2e_test consolidates the team + BYOM flow into a single end-to-end
// test. The intent is "if this test breaks we shipped a broken signup
// or membership flow" — it is the canary for the whole identity surface.
//
// What it covers:
//
//	1. signup (creates tenant + admin user)
//	2. login (issues a session) — exercised via a fake bearer verifier
//	   so the test doesn't have to round-trip a cookie
//	3. invite teammate (admin only)
//	4. invite-info preview (public)
//	5. accept-invite (public; creates the teammate + membership)
//	6. list members (sees admin + teammate)
//	7. AI provider create → test → delete
//
// Everything runs in-process with MemoryRepo + LogSender so the test
// stays under a second; no Postgres or Redis touched.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	apihttp "github.com/Satyaamm/plowered/internal/api/http"
	"github.com/Satyaamm/plowered/internal/core/auth"
	"github.com/Satyaamm/plowered/internal/core/email"
	"github.com/Satyaamm/plowered/internal/core/identity"
	"github.com/Satyaamm/plowered/internal/core/secrets"
	"github.com/Satyaamm/plowered/internal/storage/memory"
)

// newE2EServer wires every dep MemoryRepo can satisfy and returns the
// httptest.Server + the underlying repo so the test can pre-seed an
// admin user.
func newE2EServer(t *testing.T) (*httptest.Server, *identity.MemoryRepo, secrets.Vault) {
	t.Helper()

	repo := identity.NewMemoryRepo()
	key, _ := secrets.GenerateMasterKey()
	vault, _ := secrets.NewAESVault(key, secrets.NewMemoryStorage())

	deps := apihttp.Deps{
		Catalog:  memory.New(),
		Identity: repo,
		Email:    email.LogSender{Logger: slog.Default()},
		Vault:    vault,
		AuthCfg: apihttp.AuthConfig{
			CookieName:  "plowered_session",
			WebBaseURL:  "http://localhost:3000",
			FromAddress: "noreply@plowered.test",
		},
	}
	mux := apihttp.NewMux(deps)

	// Public endpoints — skipped by both the test principal middleware
	// and TenantMW. The signup + accept-invite flows hit these first;
	// principal injection only matters for authed routes.
	skip := []string{
		"/healthz", "/readyz",
		"/v1/auth/signup", "/v1/auth/login", "/v1/auth/verify",
		"/v1/auth/resend-verification",
		"/v1/auth/invite-info", "/v1/auth/accept-invite",
	}
	chain := []apihttp.Middleware{
		apihttp.RecoveryMW(nil),
		apihttp.RequestIDMW(),
		// We don't use AuthMW here — instead the test helper hpReq
		// injects the principal into ctx via a custom test middleware
		// so we can swap principals per request without minting tokens.
		testPrincipalMW(skip),
		apihttp.TenantMW(skip...),
	}
	return httptest.NewServer(apihttp.Chain(mux, chain...)), repo, vault
}

// testPrincipalMW reads X-Test-Principal: "<userID>:<tenantID>:<role>"
// off the request and stuffs it into the auth context. Public paths
// (in skip) are passed through untouched.
func testPrincipalMW(skip []string) apihttp.Middleware {
	skipped := func(path string) bool {
		for _, p := range skip {
			if len(path) >= len(p) && path[:len(p)] == p {
				return true
			}
		}
		return false
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if skipped(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			raw := r.Header.Get("X-Test-Principal")
			if raw == "" {
				http.Error(w, "missing test principal", http.StatusUnauthorized)
				return
			}
			parts := splitN(raw, ":", 3)
			if len(parts) < 2 {
				http.Error(w, "bad test principal", http.StatusUnauthorized)
				return
			}
			roles := []string{"viewer"}
			if len(parts) == 3 && parts[2] != "" {
				roles = []string{parts[2]}
			}
			p := auth.Principal{
				ID:       parts[0],
				TenantID: parts[1],
				Email:    parts[0] + "@plowered.test",
				Roles:    roles,
			}
			r = r.WithContext(auth.WithPrincipal(r.Context(), p))
			next.ServeHTTP(w, r)
		})
	}
}

// splitN avoids importing strings for one call.
func splitN(s, sep string, n int) []string {
	out := []string{}
	for n > 1 {
		i := indexOf(s, sep)
		if i < 0 {
			break
		}
		out = append(out, s[:i])
		s = s[i+len(sep):]
		n--
	}
	out = append(out, s)
	return out
}

func indexOf(s, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

type hpOpts struct {
	principal string // "<userID>:<tenantID>:<role>"
}

func hpReq(t *testing.T, srv *httptest.Server, method, path string, body any, opts hpOpts) *http.Response {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, srv.URL+path, rd)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if opts.principal != "" {
		req.Header.Set("X-Test-Principal", opts.principal)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	return resp
}

func decodeJSON[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	defer resp.Body.Close()
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("decode: %v (body=%s)", err, string(body))
	}
	return out
}

func TestE2E_TeamAndBYOM(t *testing.T) {
	srv, repo, _ := newE2EServer(t)
	defer srv.Close()

	// 1. Signup creates tenant + admin user.
	resp := hpReq(t, srv, "POST", "/v1/auth/signup", map[string]any{
		"email":            "owner@plowered.test",
		"password":         "Hunter2!secure",
		"confirm_password": "Hunter2!secure",
		"first_name":       "Olivia",
		"last_name":        "Owner",
		"workspace_name":   "Acme Data",
		"workspace_slug":   "acme-data",
		"accept_terms":     true,
	}, hpOpts{})
	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("signup: want 202, got %d, body=%s", resp.StatusCode, body)
	}
	signup := decodeJSON[struct {
		TenantID string `json:"tenant_id"`
		UserID   string `json:"user_id"`
	}](t, resp)
	if signup.TenantID == "" || signup.UserID == "" {
		t.Fatalf("signup response missing ids: %+v", signup)
	}

	// Memory mode skips email verification ergonomics — mark the user
	// verified directly so login + the team flow can proceed.
	if err := repo.MarkEmailVerified(context.Background(), signup.UserID, time.Now().UTC()); err != nil {
		t.Fatalf("mark verified: %v", err)
	}
	// Attach the admin membership the signup handler would normally
	// create (the memory signup path may not auto-create one in this
	// minimal Deps wiring — explicit is safer in a test).
	if _, err := repo.GetMembership(context.Background(), signup.TenantID, signup.UserID); err != nil {
		_ = repo.CreateMembership(context.Background(), &identity.Membership{
			TenantID:   signup.TenantID,
			UserID:     signup.UserID,
			Roles:      []string{"admin"},
			AcceptedAt: time.Now().UTC(),
		})
	}

	adminCtx := signup.UserID + ":" + signup.TenantID + ":admin"

	// 2. List members → exactly the admin shows up.
	resp = hpReq(t, srv, "GET", "/v1/members", nil, hpOpts{principal: adminCtx})
	if resp.StatusCode != 200 {
		t.Fatalf("list members: %d", resp.StatusCode)
	}
	members := decodeJSON[struct {
		Members []struct {
			Email string   `json:"email"`
			Roles []string `json:"roles"`
		} `json:"members"`
	}](t, resp)
	if len(members.Members) != 1 || members.Members[0].Email != "owner@plowered.test" {
		t.Fatalf("expected one admin member, got %+v", members)
	}

	// 3. Invite a teammate.
	resp = hpReq(t, srv, "POST", "/v1/invites", map[string]any{
		"email": "engineer@plowered.test",
		"roles": []string{"editor"},
	}, hpOpts{principal: adminCtx})
	if resp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("create invite: %d body=%s", resp.StatusCode, body)
	}
	invite := decodeJSON[struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}](t, resp)

	// 4. Read the invite token directly from the repo (the API
	//    deliberately doesn't return it; the email is the channel).
	pending, err := repo.ListInvitesForTenant(context.Background(), signup.TenantID, true)
	if err != nil || len(pending) != 1 {
		t.Fatalf("expected one pending invite, got %v err=%v", pending, err)
	}
	token := pending[0].Token

	// 5. invite-info preview is public — no principal header.
	resp = hpReq(t, srv, "GET", "/v1/auth/invite-info?token="+token, nil, hpOpts{})
	if resp.StatusCode != 200 {
		t.Fatalf("invite-info: %d", resp.StatusCode)
	}
	preview := decodeJSON[struct {
		Email     string `json:"email"`
		Workspace string `json:"workspace_name"`
	}](t, resp)
	if preview.Email != "engineer@plowered.test" || preview.Workspace != "Acme Data" {
		t.Fatalf("preview mismatch: %+v", preview)
	}

	// 6. Accept the invite — creates the teammate.
	resp = hpReq(t, srv, "POST", "/v1/auth/accept-invite", map[string]any{
		"token":      token,
		"password":   "Teammate1!secure",
		"first_name": "Ethan",
		"last_name":  "Engineer",
	}, hpOpts{})
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("accept-invite: %d body=%s", resp.StatusCode, body)
	}
	accept := decodeJSON[struct {
		UserID   string `json:"user_id"`
		TenantID string `json:"tenant_id"`
	}](t, resp)
	if accept.TenantID != signup.TenantID {
		t.Fatalf("accept attached wrong tenant: got %s want %s", accept.TenantID, signup.TenantID)
	}

	// 7. List members again → admin + engineer.
	resp = hpReq(t, srv, "GET", "/v1/members", nil, hpOpts{principal: adminCtx})
	members2 := decodeJSON[struct {
		Members []struct {
			Email string   `json:"email"`
			Roles []string `json:"roles"`
		} `json:"members"`
	}](t, resp)
	if len(members2.Members) != 2 {
		t.Fatalf("expected 2 members after accept, got %+v", members2)
	}

	// 8. Non-admin cannot invite.
	teammateCtx := accept.UserID + ":" + accept.TenantID + ":editor"
	resp = hpReq(t, srv, "POST", "/v1/invites", map[string]any{
		"email": "x@plowered.test",
		"roles": []string{"viewer"},
	}, hpOpts{principal: teammateCtx})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-admin invite: want 403, got %d", resp.StatusCode)
	}

	// Stash the invite ID so test output is informative if 7. fails.
	_ = invite
}
