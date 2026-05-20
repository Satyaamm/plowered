package http

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/Satyaamm/plowered/internal/core/cost"
)

func costHandlers(mux *http.ServeMux, r cost.Reader, b cost.BudgetStore) {
	if r == nil {
		return
	}
	mux.HandleFunc("GET /v1/cost/recent",  recentCostHandler(r))
	mux.HandleFunc("GET /v1/cost/summary", summaryCostHandler(r))
	if b != nil {
		mux.HandleFunc("GET  /v1/cost/budget", getBudgetHandler(b))
		mux.HandleFunc("POST /v1/cost/budget", upsertBudgetHandler(b))
	}
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
		// Per-feature breakdown comes from the recent records — we walk
		// up to 1000 rows in the same window. Heavier than the daily
		// aggregate but the table tops out at a few hundred per day in
		// v0 so the cost is bounded.
		recent, _ := r.Recent(req.Context(), tenant, 1000)
		byFeature := map[string]float64{}
		for _, rec := range recent {
			feature, _ := rec.Attributes["feature"].(string)
			if feature == "" {
				feature = "unknown"
			}
			byFeature[feature] += rec.CostUSD
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"daily":        daily,
			"by_kind":      byKind,
			"by_provider":  byProvider,
			"by_feature":   byFeature,
			"total_usd":    grand,
		})
	}
}

type budgetReq struct {
	MonthlyUSD *float64 `json:"monthly_usd"`
	WarnAtPct  int      `json:"warn_at_pct"`
	HardAtPct  int      `json:"hard_at_pct"`
}

func getBudgetHandler(s cost.BudgetStore) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		tenant := mustTenant(w, req)
		if tenant == "" {
			return
		}
		b, err := s.GetBudget(req.Context(), tenant)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody{"cost_error", err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, b)
	}
}

func upsertBudgetHandler(s cost.BudgetStore) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		tenant := mustTenant(w, req)
		if tenant == "" {
			return
		}
		var body budgetReq
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		b, err := s.UpsertBudget(req.Context(), &cost.Budget{
			TenantID:   tenant,
			MonthlyUSD: body.MonthlyUSD,
			WarnAtPct:  body.WarnAtPct,
			HardAtPct:  body.HardAtPct,
		})
		if err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"cost_error", err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, b)
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
