package http

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Satyaamm/plowered/internal/core/aiprovider"
	"github.com/Satyaamm/plowered/internal/core/describer"
)

// Describer is the small surface the HTTP layer needs from the
// describer service. Decoupling from the concrete *describer.Service
// keeps handler tests stub-able.
type Describer interface {
	Suggest(ctx context.Context, tenantID, assetID, generatedBy string) (*describer.Suggestion, error)
}

func describeHandlers(mux *http.ServeMux, d Describer) {
	if d == nil {
		return
	}
	mux.HandleFunc("POST /v1/assets/{id}/describe:ai", describeAssetHandler(d))
}

func describeAssetHandler(d Describer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		actor := ""
		if pr, ok := principalFrom(r); ok {
			actor = pr.ID
		}
		// 90s budget — chat providers occasionally take >30s on cold
		// starts; we'd rather wait than 504.
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		sug, err := d.Suggest(ctx, tenant, r.PathValue("id"), actor)
		if err != nil {
			writeDescribeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, sug)
	}
}

func writeDescribeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, aiprovider.ErrNoPrimary):
		writeJSON(w, http.StatusBadRequest, errorBody{
			"no_ai_provider",
			"No primary chat provider configured. Add one in Settings → AI providers.",
		})
	default:
		writeJSON(w, http.StatusInternalServerError, errorBody{
			"describe_failed",
			err.Error(),
		})
	}
}
