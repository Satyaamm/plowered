// Package http exposes Plowered's catalog over a JSON REST API. It is the
// surface the web UI and any third-party integrations talk to today; the
// proto-defined gRPC surface fills in alongside once `buf generate` runs.
//
// All handlers run behind the same auth + tenant + audit chain as the gRPC
// server (see internal/api/middleware), translated to net/http.
package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Satyaamm/plowered/internal/core/graph"
	"github.com/Satyaamm/plowered/internal/storage"
)

// Mux returns an *http.ServeMux with every catalog/lineage/context route
// registered. Callers may add more routes (signup, admin, etc.) before
// wrapping the result in the auth/tenant/audit chain.
func Mux(store storage.Store) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /v1/assets",                     listAssetsHandler(store))
	mux.HandleFunc("POST /v1/assets",                    createAssetHandler(store))
	mux.HandleFunc("GET /v1/assets/{id}",                getAssetHandler(store))
	mux.HandleFunc("PATCH /v1/assets/{id}",              updateAssetHandler(store))
	mux.HandleFunc("DELETE /v1/assets/{id}",             deleteAssetHandler(store))
	mux.HandleFunc("GET /v1/assets:byQualifiedName",     getByQNHandler(store))
	mux.HandleFunc("POST /v1/assets:search",             searchAssetsHandler(store))

	mux.HandleFunc("GET /v1/assets/{id}/lineage",        lineageHandler(store))

	return mux
}

// ----- response helpers -----

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, graph.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{"not_found", err.Error()})
	case errors.Is(err, graph.ErrConflict):
		writeJSON(w, http.StatusConflict, errorBody{"conflict", err.Error()})
	case errors.Is(err, graph.ErrInvalidArgument):
		writeJSON(w, http.StatusBadRequest, errorBody{"invalid_argument", err.Error()})
	case errors.Is(err, graph.ErrForbidden):
		writeJSON(w, http.StatusForbidden, errorBody{"forbidden", err.Error()})
	case errors.Is(err, graph.ErrTenantMissing):
		writeJSON(w, http.StatusUnauthorized, errorBody{"tenant_required", err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, errorBody{"internal", "internal error"})
	}
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	return nil
}
