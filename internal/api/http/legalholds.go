package http

import (
	"net/http"
	"time"

	"github.com/Satyaamm/plowered/internal/core/auth"
	"github.com/Satyaamm/plowered/internal/core/legalhold"
	"github.com/Satyaamm/plowered/internal/core/policy"
)

// legalHoldHandlers exposes the litigation-hold admin surface.
// Read + write both require admin — pending holds tell an attacker
// which entities are subject to litigation, and issuing / releasing is
// an admin chain-of-custody action.
func legalHoldHandlers(mux *http.ServeMux, repo legalhold.Repo, authz policy.Authorizer) {
	mux.HandleFunc("GET /v1/legal-holds",                 listLegalHoldsHandler(repo, authz))
	mux.HandleFunc("POST /v1/legal-holds",                issueLegalHoldHandler(repo, authz))
	mux.HandleFunc("POST /v1/legal-holds/{id}/release",   releaseLegalHoldHandler(repo, authz))
}

func listLegalHoldsHandler(repo legalhold.Repo, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := gateTenantAndVerb(w, r, authz, policy.VerbAdmin, "legal_hold")
		if tenant == "" {
			return
		}
		holds, err := repo.List(r.Context(), tenant)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"holds": holds})
	}
}

func issueLegalHoldHandler(repo legalhold.Repo, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := gateTenantAndVerb(w, r, authz, policy.VerbAdmin, "legal_hold")
		if tenant == "" {
			return
		}
		p, _ := auth.PrincipalFromContext(r.Context())
		var h legalhold.Hold
		if err := decodeJSON(r, &h); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if h.Matter == "" {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "matter is required"})
			return
		}
		h.TenantID = tenant
		h.IssuedBy = p.ID
		out, err := repo.Issue(r.Context(), &h)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, out)
	}
}

func releaseLegalHoldHandler(repo legalhold.Repo, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := gateTenantAndVerb(w, r, authz, policy.VerbAdmin, "legal_hold")
		if tenant == "" {
			return
		}
		p, _ := auth.PrincipalFromContext(r.Context())
		id := r.PathValue("id")
		if err := repo.Release(r.Context(), tenant, id, p.ID, time.Now().UTC()); err != nil {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
