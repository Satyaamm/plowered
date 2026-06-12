package http

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Satyaamm/plowered/internal/core/policy"
	"github.com/Satyaamm/plowered/internal/core/profile"
	"github.com/Satyaamm/plowered/internal/core/warehouse"
)

// Profiler is the small surface the HTTP layer needs from the profile
// service. Decoupled from the concrete *profile.Service so handlers
// stay testable.
type Profiler interface {
	Get(ctx context.Context, tenantID, tableAssetID string) (*profile.Report, error)
	Refresh(ctx context.Context, tenantID, tableAssetID string) (*profile.Report, error)
}

func profileHandlers(mux *http.ServeMux, p Profiler, authz policy.Authorizer) {
	if p == nil {
		return
	}
	mux.HandleFunc("GET /v1/assets/{id}/profile", profileGetHandler(p, authz))
	mux.HandleFunc("POST /v1/assets/{id}/profile:refresh", profileRefreshHandler(p, authz))
}

func profileGetHandler(p Profiler, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := gateTenantAndVerb(w, r, authz, policy.VerbRead, "asset")
		if tenant == "" {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		report, err := p.Get(ctx, tenant, r.PathValue("id"))
		if err != nil {
			writeProfileError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, report)
	}
}

func profileRefreshHandler(p Profiler, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Refresh re-scans the warehouse for nulls + top-k + histograms.
		// VerbRun gates the warehouse work to editor+.
		tenant := gateTenantAndVerb(w, r, authz, policy.VerbRun, "asset")
		if tenant == "" {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
		defer cancel()
		report, err := p.Refresh(ctx, tenant, r.PathValue("id"))
		if err != nil {
			writeProfileError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, report)
	}
}

// writeProfileError maps the service's domain errors to friendly HTTP
// responses. Untyped errors land as 500.
func writeProfileError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, warehouse.ErrUnsupportedType):
		writeJSON(w, http.StatusBadRequest, errorBody{
			"profile_unsupported",
			"This source type doesn't support SQL profiling (document stores like DynamoDB / Mongo)",
		})
	case errors.Is(err, warehouse.ErrDriverNotInstalled):
		writeJSON(w, http.StatusBadRequest, errorBody{
			"driver_not_installed",
			"The driver for this connection is not compiled into this build",
		})
	default:
		writeJSON(w, http.StatusInternalServerError, errorBody{
			"profile_failed",
			err.Error(),
		})
	}
}
