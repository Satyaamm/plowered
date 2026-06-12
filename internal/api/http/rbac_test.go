package http_test

// Endpoint-level RBAC enforcement tests.
//
// One representative mutating endpoint per gated domain. Asserts:
//
//   - viewer  → 403 (cannot mutate)
//   - admin   → not 403 (the request is allowed past authz; subsequent
//               business-logic responses like 400/404 are acceptable since
//               we're only validating the gate)
//
// Adding a new gated domain? Drop a row into the `cases` table below.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	apihttp "github.com/Satyaamm/plowered/internal/api/http"
	"github.com/Satyaamm/plowered/internal/core/audit"
	"github.com/Satyaamm/plowered/internal/core/auth"
	"github.com/Satyaamm/plowered/internal/core/deleted"
	"github.com/Satyaamm/plowered/internal/core/dsr"
	"github.com/Satyaamm/plowered/internal/core/identity"
	"github.com/Satyaamm/plowered/internal/core/legalhold"
	"github.com/Satyaamm/plowered/internal/core/notify"
	"github.com/Satyaamm/plowered/internal/core/pipeline"
	"github.com/Satyaamm/plowered/internal/core/policy"
	"github.com/Satyaamm/plowered/internal/core/quality"
	"github.com/Satyaamm/plowered/internal/storage/memory"
)

// principalForRole returns a Principal in tenant t1 with the given role.
func principalForRole(role string) auth.Principal {
	return auth.Principal{
		ID:       "u-" + role,
		TenantID: "t1",
		Email:    role + "@plowered.test",
		Roles:    []string{role},
	}
}

// newRBACServer wires the gated handlers under an Auth + Tenant
// middleware that maps the bearer token to the role of the same name
// (so a "viewer" token authenticates as a viewer).
//
// Wires only memory-backed services. Contract / certification / cost
// are Postgres-only today, so their endpoints are validated by
// integration tests against the real store rather than here.
func newRBACServer(t *testing.T) *httptest.Server {
	t.Helper()
	auditWriter := audit.NewMemoryWriter()
	deps := apihttp.Deps{
		Catalog:     memory.New(),
		Pipelines:   pipeline.NewMemoryStore(),
		Quality:     quality.NewMemoryStore(),
		Notify:      notify.NewMemoryStore(),
		Policies:    policy.NewMemoryRuleStore(),
		Deleted:     deleted.NewMemoryRepo(),
		LegalHolds:  legalhold.NewMemoryRepo(),
		DSR:         dsr.NewMemoryRepo(),
		Audit:       auditWriter,
		AuditWriter: auditWriter,
		Identity:    identity.NewMemoryRepo(),
	}
	mux := apihttp.NewMux(deps)
	h := apihttp.Chain(mux,
		apihttp.AuthMW(func(token string) (auth.Principal, error) {
			return principalForRole(token), nil
		}),
		apihttp.TenantMW(),
	)
	return httptest.NewServer(h)
}

// rbacCase declares one HTTP call gated by RBAC. allowedRoles is what
// the gate should let through; everyone else must get 403.
type rbacCase struct {
	name         string
	method       string
	path         string
	body         any
	allowedRoles []string
}

// allRoles is the closed set the matrix in policy.go covers today.
var allRoles = []string{"viewer", "editor", "steward", "admin", "super_admin"}

func TestEndpointRBAC(t *testing.T) {
	s := newRBACServer(t)
	defer s.Close()

	cases := []rbacCase{
		// catalog ---------------------------------------------------------
		{
			name:         "catalog.assets.list requires read (any role)",
			method:       "GET",
			path:         "/v1/assets",
			body:         nil,
			allowedRoles: []string{"viewer", "editor", "steward", "admin", "super_admin"},
		},
		// stats -----------------------------------------------------------
		{
			name:         "stats.get requires read",
			method:       "GET",
			path:         "/v1/stats",
			body:         nil,
			allowedRoles: []string{"viewer", "editor", "steward", "admin", "super_admin"},
		},
		// jobs ------------------------------------------------------------
		// (Skipped: jobs is wired only when d.Jobs is set; no memory
		// repo today. Coverage lives in integration tests.)
		// policies (meta-RBAC) -------------------------------------------
		{
			name:         "policies.list requires admin (sensitive rules)",
			method:       "GET",
			path:         "/v1/policies",
			body:         nil,
			allowedRoles: []string{"admin", "super_admin"},
		},
		{
			name:         "policies.create requires admin",
			method:       "POST",
			path:         "/v1/policies",
			body:         map[string]any{"effect": "allow", "verbs": []string{"read"}},
			allowedRoles: []string{"admin", "super_admin"},
		},
		{
			name:         "catalog.assets.create requires editor+",
			method:       "POST",
			path:         "/v1/assets",
			body:         map[string]any{"qualified_name": "warehouse://t.users", "type": "table", "name": "users"},
			allowedRoles: []string{"editor", "steward", "admin", "super_admin"},
		},
		// pipelines -------------------------------------------------------
		{
			name:         "pipelines.create requires editor+",
			method:       "POST",
			path:         "/v1/pipelines",
			body:         map[string]any{"name": "ingest"},
			allowedRoles: []string{"editor", "steward", "admin", "super_admin"},
		},
		{
			name:         "pipelines.trigger requires run verb (editor+)",
			method:       "POST",
			path:         "/v1/pipelines/missing/trigger",
			body:         nil,
			allowedRoles: []string{"editor", "steward", "admin", "super_admin"},
		},
		// quality checks --------------------------------------------------
		{
			name:         "checks.create requires editor+",
			method:       "POST",
			path:         "/v1/checks",
			body:         map[string]any{"name": "rows", "type": "row_count", "asset_id": "a1"},
			allowedRoles: []string{"editor", "steward", "admin", "super_admin"},
		},
		{
			name:         "checks.run requires editor+ (run verb)",
			method:       "POST",
			path:         "/v1/checks/missing/run",
			body:         nil,
			allowedRoles: []string{"editor", "steward", "admin", "super_admin"},
		},
		// audit -----------------------------------------------------------
		{
			name:         "audit.list requires admin",
			method:       "GET",
			path:         "/v1/audit",
			body:         nil,
			allowedRoles: []string{"admin", "super_admin"},
		},
		// recycle bin -----------------------------------------------------
		{
			name:         "deleted.list requires admin",
			method:       "GET",
			path:         "/v1/deleted",
			body:         nil,
			allowedRoles: []string{"admin", "super_admin"},
		},
		{
			name:         "deleted.purge requires super_admin (VerbPurge)",
			method:       "DELETE",
			path:         "/v1/deleted/missing",
			body:         nil,
			allowedRoles: []string{"super_admin"},
		},
		// legal holds -----------------------------------------------------
		{
			name:         "legal-holds.list requires admin",
			method:       "GET",
			path:         "/v1/legal-holds",
			body:         nil,
			allowedRoles: []string{"admin", "super_admin"},
		},
		{
			name:         "legal-holds.issue requires admin",
			method:       "POST",
			path:         "/v1/legal-holds",
			body:         map[string]any{"matter": "litigation-2026", "scope": map[string]any{"resource_types": []string{"asset"}}},
			allowedRoles: []string{"admin", "super_admin"},
		},
		// DSR -------------------------------------------------------------
		{
			name:         "dsr.list requires admin",
			method:       "GET",
			path:         "/v1/dsr",
			body:         nil,
			allowedRoles: []string{"admin", "super_admin"},
		},
		{
			name:         "dsr.create requires admin",
			method:       "POST",
			path:         "/v1/dsr",
			body:         map[string]any{"subject_id": "user-1", "type": "access"},
			allowedRoles: []string{"admin", "super_admin"},
		},
		// team ------------------------------------------------------------
		{
			name:         "team.invites.create requires admin",
			method:       "POST",
			path:         "/v1/invites",
			body:         map[string]any{"email": "new@plowered.test", "roles": []string{"viewer"}},
			allowedRoles: []string{"admin", "super_admin"},
		},
		// notify ----------------------------------------------------------
		{
			name:         "notify.channels.create requires admin+",
			method:       "POST",
			path:         "/v1/notifications/channels",
			body:         map[string]any{"kind": "log", "name": "default"},
			allowedRoles: []string{"admin", "super_admin"},
		},
		{
			name:         "notify.rules.create requires admin+",
			method:       "POST",
			path:         "/v1/notifications/rules",
			body:         map[string]any{"channel_id": "c1", "name": "all-errors"},
			allowedRoles: []string{"admin", "super_admin"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			for _, role := range allRoles {
				role := role
				t.Run(role, func(t *testing.T) {
					resp := rbacReq(t, s, role, tc.method, tc.path, tc.body)
					defer resp.Body.Close()
					allowed := contains(tc.allowedRoles, role)
					if allowed && resp.StatusCode == http.StatusForbidden {
						t.Fatalf("%s should be allowed but got 403", role)
					}
					if !allowed && resp.StatusCode != http.StatusForbidden {
						t.Fatalf("%s should be blocked but got %d", role, resp.StatusCode)
					}
				})
			}
		})
	}
}

// TestEndpointRBAC_NoPrincipalIsRejected confirms an unauthenticated
// request gets 401, not 403.
func TestEndpointRBAC_NoPrincipalIsRejected(t *testing.T) {
	store := memory.New()
	mux := apihttp.NewMux(apihttp.Deps{Catalog: store})
	// Wrap with tenant middleware only — no auth. The handler should see
	// no principal and reject.
	h := apihttp.Chain(mux, apihttp.TenantMW())
	s := httptest.NewServer(h)
	defer s.Close()

	r, _ := http.NewRequest("POST", s.URL+"/v1/assets",
		bytes.NewReader([]byte(`{"qualified_name":"x","type":"table","name":"x"}`)))
	r.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func rbacReq(t *testing.T, s *httptest.Server, role, method, path string, body any) *http.Response {
	t.Helper()
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	r, _ := http.NewRequest(method, s.URL+path, rd)
	r.Header.Set("Authorization", "Bearer "+role)
	r.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatalf("%s %s as %s: %v", method, path, role, err)
	}
	return resp
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
