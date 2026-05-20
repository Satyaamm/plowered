package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/Satyaamm/plowered/internal/core/contract"
)

func contractHandlers(mux *http.ServeMux, svc *contract.Service) {
	if svc == nil {
		return
	}
	mux.HandleFunc("GET    /v1/contracts",                          listContractsHandler(svc))
	mux.HandleFunc("POST   /v1/contracts",                          upsertContractHandler(svc))
	mux.HandleFunc("GET    /v1/contracts/{id}",                     getContractHandler(svc))
	mux.HandleFunc("DELETE /v1/contracts/{id}",                     deleteContractHandler(svc))
	mux.HandleFunc("POST   /v1/contracts/{id}/evaluate",            evaluateContractHandler(svc))
	mux.HandleFunc("GET    /v1/contracts/{id}/breaches",            contractBreachesHandler(svc))
	mux.HandleFunc("GET    /v1/contracts/breaches",                 tenantBreachesHandler(svc))
	mux.HandleFunc("POST   /v1/contracts/evaluate",                 evaluateAllHandler(svc))
	mux.HandleFunc("GET    /v1/assets/{id}/contract",               assetContractHandler(svc))
}

func listContractsHandler(svc *contract.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		list, err := svc.List(r.Context(), tenant)
		if err != nil {
			writeContractError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"contracts": list})
	}
}

type upsertContractReq struct {
	AssetID          string                       `json:"asset_id"`
	Status           contract.Status              `json:"status"`
	ExpectedColumns  []contract.ExpectedColumn    `json:"expected_columns"`
	FreshnessSeconds int                          `json:"freshness_seconds"`
	NullThresholds   map[string]float64           `json:"null_thresholds"`
	Description      string                       `json:"description"`
}

func upsertContractHandler(svc *contract.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant, actor := tenantAndActor(w, r)
		if tenant == "" {
			return
		}
		var body upsertContractReq
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		c, err := svc.Upsert(r.Context(), &contract.Contract{
			TenantID:         tenant,
			AssetID:          body.AssetID,
			OwnerID:          actor,
			Status:           body.Status,
			ExpectedColumns:  body.ExpectedColumns,
			FreshnessSeconds: body.FreshnessSeconds,
			NullThresholds:   body.NullThresholds,
			Description:      body.Description,
		})
		if err != nil {
			writeContractError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, c)
	}
}

func getContractHandler(svc *contract.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		c, err := svc.Get(r.Context(), tenant, r.PathValue("id"))
		if err != nil {
			writeContractError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, c)
	}
}

func deleteContractHandler(svc *contract.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		if err := svc.Delete(r.Context(), tenant, r.PathValue("id")); err != nil {
			writeContractError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func evaluateContractHandler(svc *contract.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		breaches, err := svc.Evaluate(r.Context(), tenant, r.PathValue("id"))
		if err != nil {
			writeContractError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"breaches": breaches,
			"count":    len(breaches),
		})
	}
}

func evaluateAllHandler(svc *contract.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		count, err := svc.EvaluateAll(r.Context(), tenant)
		if err != nil {
			writeContractError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"breach_count": count})
	}
}

func contractBreachesHandler(svc *contract.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		breaches, err := svc.BreachesForContract(r.Context(), tenant, r.PathValue("id"), limit)
		if err != nil {
			writeContractError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"breaches": breaches})
	}
}

func tenantBreachesHandler(svc *contract.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		breaches, err := svc.Breaches(r.Context(), tenant, limit)
		if err != nil {
			writeContractError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"breaches": breaches})
	}
}

func assetContractHandler(svc *contract.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		c, err := svc.GetByAsset(r.Context(), tenant, r.PathValue("id"))
		if err != nil {
			writeContractError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, c)
	}
}

func writeContractError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, contract.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{"not_found", err.Error()})
	default:
		writeJSON(w, http.StatusBadRequest, errorBody{"contract_error", err.Error()})
	}
}
