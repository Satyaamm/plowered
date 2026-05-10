package http

import (
	"context"
	"net/http"

	"github.com/Satyaamm/plowered/internal/storage/postgres"
)

// ColumnLineageReader is the read interface the API needs. The Postgres
// implementation in internal/storage/postgres satisfies it; tests can
// substitute an in-memory recorder.
type ColumnLineageReader interface {
	ListByAsset(ctx context.Context, tenantID, assetID string) ([]postgres.ColumnLineageView, error)
}

func columnLineageHandlers(mux *http.ServeMux, r ColumnLineageReader) {
	mux.HandleFunc("GET /v1/assets/{id}/column-lineage", listColumnLineageHandler(r))
}

func listColumnLineageHandler(r ColumnLineageReader) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		tenant := mustTenant(w, req)
		if tenant == "" {
			return
		}
		id := req.PathValue("id")
		edges, err := r.ListByAsset(req.Context(), tenant, id)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"edges": edges})
	}
}
