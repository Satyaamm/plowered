package http

import (
	"net/http"
	"strconv"

	"github.com/Satyaamm/plowered/internal/core/audit"
)

func auditHandlers(mux *http.ServeMux, reader audit.Reader) {
	mux.HandleFunc("GET /v1/audit", listAuditHandler(reader))
}

func listAuditHandler(r audit.Reader) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		tenant := mustTenant(w, req)
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
