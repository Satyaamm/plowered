package http

import (
	"net/http"
	"strconv"
	"time"

	"github.com/Satyaamm/plowered/internal/core/cost"
)

func costHandlers(mux *http.ServeMux, r cost.Reader) {
	if r == nil {
		return
	}
	mux.HandleFunc("GET /v1/cost/recent",  recentCostHandler(r))
	mux.HandleFunc("GET /v1/cost/summary", summaryCostHandler(r))
}

func recentCostHandler(r cost.Reader) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		tenant := mustTenant(w, req)
		if tenant == "" {
			return
		}
		limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
		recs, err := r.Recent(req.Context(), tenant, limit)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody{"cost_error", err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"records": recs})
	}
}

func summaryCostHandler(r cost.Reader) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		tenant := mustTenant(w, req)
		if tenant == "" {
			return
		}
		from := parseTime(req.URL.Query().Get("from"))
		to := parseTime(req.URL.Query().Get("to"))
		daily, err := r.Daily(req.Context(), tenant, from, to)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody{"cost_error", err.Error()})
			return
		}
		// Roll up to (kind, provider) totals + grand total alongside the
		// per-day series so the dashboard can render headline numbers
		// without crunching the array itself.
		byKind := map[string]float64{}
		byProvider := map[string]float64{}
		var grand float64
		for _, d := range daily {
			byKind[string(d.Kind)] += d.CostUSD
			byProvider[d.Provider] += d.CostUSD
			grand += d.CostUSD
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"daily":        daily,
			"by_kind":      byKind,
			"by_provider":  byProvider,
			"total_usd":    grand,
		})
	}
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}
