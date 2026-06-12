package http

import (
	"net/http"

	"github.com/Satyaamm/plowered/internal/core/auth"
	"github.com/Satyaamm/plowered/internal/core/policy"
)

// authzGate is the per-process authorizer initialized in NewMux. Helpers
// reach for this through the closure NewMux hands them, so handlers don't
// have to thread it through every signature.

// gate is the standard authorization checkpoint for HTTP handlers. Call
// it right after mustTenant. It writes 403 (with the engine's reason
// string) and returns false when the principal cannot perform verb on a
// resource of resourceType in their tenant.
//
// For endpoints that need richer resource context (tags, owner ids), use
// gateResource instead.
//
// authz may be nil — that means RBAC is disabled (memory mode / tests).
// In that case the gate always allows.
func gate(
	w http.ResponseWriter,
	r *http.Request,
	authz policy.Authorizer,
	verb policy.Verb,
	resourceType string,
) bool {
	if authz == nil {
		return true
	}
	p, err := auth.PrincipalFromContext(r.Context())
	if err != nil || p.ID == "" {
		writeJSON(w, http.StatusUnauthorized, errorBody{"unauthorized", "no principal"})
		return false
	}
	res := policy.Resource{Type: resourceType, TenantID: p.TenantID}
	d := authz.Allow(r.Context(), p, verb, res)
	if !d.Allow {
		writeJSON(w, http.StatusForbidden, errorBody{"forbidden", d.Reason})
		return false
	}
	return true
}

// gateResource is like gate but lets the caller pass a fully-built
// Resource (with tags / owner ids) so per-resource Rules can apply.
func gateResource(
	w http.ResponseWriter,
	r *http.Request,
	authz policy.Authorizer,
	verb policy.Verb,
	res policy.Resource,
) bool {
	if authz == nil {
		return true
	}
	p, err := auth.PrincipalFromContext(r.Context())
	if err != nil || p.ID == "" {
		writeJSON(w, http.StatusUnauthorized, errorBody{"unauthorized", "no principal"})
		return false
	}
	if res.TenantID == "" {
		res.TenantID = p.TenantID
	}
	d := authz.Allow(r.Context(), p, verb, res)
	if !d.Allow {
		writeJSON(w, http.StatusForbidden, errorBody{"forbidden", d.Reason})
		return false
	}
	return true
}

// gateTenantAndVerb combines mustTenant + gate into one call for the
// common path: "give me the tenant, but only after confirming the
// principal can do verb on resourceType". Returns "" when the response
// has already been written.
func gateTenantAndVerb(
	w http.ResponseWriter,
	r *http.Request,
	authz policy.Authorizer,
	verb policy.Verb,
	resourceType string,
) string {
	tenant := mustTenant(w, r)
	if tenant == "" {
		return ""
	}
	if !gate(w, r, authz, verb, resourceType) {
		return ""
	}
	return tenant
}

// gateTenantActorAndVerb is the same as gateTenantAndVerb but also returns
// the principal id (the actor) for write paths that record it on rows.
func gateTenantActorAndVerb(
	w http.ResponseWriter,
	r *http.Request,
	authz policy.Authorizer,
	verb policy.Verb,
	resourceType string,
) (string, string) {
	tenant, actor := tenantAndActor(w, r)
	if tenant == "" {
		return "", ""
	}
	if !gate(w, r, authz, verb, resourceType) {
		return "", ""
	}
	return tenant, actor
}
