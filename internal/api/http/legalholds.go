package http

import (
	"net/http"
	"time"

	"github.com/Satyaamm/plowered/internal/core/auth"
	"github.com/Satyaamm/plowered/internal/core/legalhold"
	"github.com/Satyaamm/plowered/internal/core/policy"
)

// legalHoldHandlers exposes the litigation-hold admin surface.
// Issuing or releasing a hold is restricted to admin+; listing is open to
// any authenticated principal so engineers can self-diagnose 409s.
func legalHoldHandlers(mux *http.ServeMux, repo legalhold.Repo) {
	mux.HandleFunc("GET /v1/legal-holds",            listLegalHoldsHandler(repo))
	mux.HandleFunc("POST /v1/legal-holds",           issueLegalHoldHandler(repo))
	mux.HandleFunc("POST /v1/legal-holds/{id}/release", releaseLegalHoldHandler(repo))
}

func listLegalHoldsHandler(repo legalhold.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
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

func issueLegalHoldHandler(repo legalhold.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		p, _ := auth.PrincipalFromContext(r.Context())
		if !policy.HasRole(p, "admin") && !policy.HasRole(p, "super_admin") {
			writeJSON(w, http.StatusForbidden, errorBody{"forbidden",
				"only admin/super_admin can issue a legal hold"})
			return
		}
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

func releaseLegalHoldHandler(repo legalhold.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		p, _ := auth.PrincipalFromContext(r.Context())
		if !policy.HasRole(p, "admin") && !policy.HasRole(p, "super_admin") {
			writeJSON(w, http.StatusForbidden, errorBody{"forbidden",
				"only admin/super_admin can release a legal hold"})
			return
		}
		id := r.PathValue("id")
		if err := repo.Release(r.Context(), tenant, id, p.ID, time.Now().UTC()); err != nil {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
