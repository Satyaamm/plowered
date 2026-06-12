package http

import (
	"net/http"
	"strconv"

	"github.com/Satyaamm/plowered/internal/core/audit"
	"github.com/Satyaamm/plowered/internal/core/policy"
)

func auditHandlers(mux *http.ServeMux, reader audit.Reader, authz policy.Authorizer) {
	mux.HandleFunc("GET /v1/audit", listAuditHandler(reader, authz))
}

func listAuditHandler(r audit.Reader, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		// Audit log carries who-did-what across the tenant — admin only.
		tenant := gateTenantAndVerb(w, req, authz, policy.VerbAdmin, "audit_event")
		if tenant == "" {
			return
		}
		limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
		events, err := r.List(req.Context(), tenant, limit)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"events": events})
	}
}
