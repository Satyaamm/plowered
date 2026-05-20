package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Satyaamm/plowered/internal/core/certification"
)

func certificationHandlers(mux *http.ServeMux, svc *certification.Service) {
	if svc == nil {
		return
	}
	// Per-asset surface: history + propose
	mux.HandleFunc("GET  /v1/assets/{id}/certifications",   listAssetCertsHandler(svc))
	mux.HandleFunc("POST /v1/assets/{id}/certifications",   proposeCertHandler(svc))
	// Cross-asset review surface
	mux.HandleFunc("GET  /v1/certifications/pending",       pendingCertsHandler(svc))
	mux.HandleFunc("POST /v1/certifications/{id}/approve",  approveCertHandler(svc))
	mux.HandleFunc("POST /v1/certifications/{id}/reject",   rejectCertHandler(svc))
	mux.HandleFunc("POST /v1/assets/{id}/certifications/revoke", revokeCertHandler(svc))
}

type proposeCertReq struct {
	Justification string `json:"justification"`
}
type reviewReq struct {
	Note string `json:"note"`
}

func listAssetCertsHandler(svc *certification.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		history, err := svc.History(r.Context(), tenant, r.PathValue("id"))
		if err != nil {
			writeCertError(w, err)
			return
		}
		latest, _ := svc.Latest(r.Context(), tenant, r.PathValue("id"))
		writeJSON(w, http.StatusOK, map[string]any{
			"latest":  latest,
			"history": history,
		})
	}
}

func proposeCertHandler(svc *certification.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		var body proposeCertReq
		_ = json.NewDecoder(r.Body).Decode(&body)
		actor := ""
		if pr, ok := principalFrom(r); ok {
			actor = pr.ID
		}
		c, err := svc.Propose(r.Context(), tenant, r.PathValue("id"), actor, body.Justification)
		if err != nil {
			writeCertError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, c)
	}
}

func pendingCertsHandler(svc *certification.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		pending, err := svc.Pending(r.Context(), tenant, 100)
		if err != nil {
			writeCertError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"pending": pending})
	}
}

func approveCertHandler(svc *certification.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant, actor := tenantAndActor(w, r)
		if tenant == "" {
			return
		}
		var body reviewReq
		_ = json.NewDecoder(r.Body).Decode(&body)
		c, err := svc.Approve(r.Context(), tenant, r.PathValue("id"), actor, body.Note)
		if err != nil {
			writeCertError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, c)
	}
}

func rejectCertHandler(svc *certification.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant, actor := tenantAndActor(w, r)
		if tenant == "" {
			return
		}
		var body reviewReq
		_ = json.NewDecoder(r.Body).Decode(&body)
		c, err := svc.Reject(r.Context(), tenant, r.PathValue("id"), actor, body.Note)
		if err != nil {
			writeCertError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, c)
	}
}

func revokeCertHandler(svc *certification.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant, actor := tenantAndActor(w, r)
		if tenant == "" {
			return
		}
		var body reviewReq
		_ = json.NewDecoder(r.Body).Decode(&body)
		c, err := svc.Revoke(r.Context(), tenant, r.PathValue("id"), actor, body.Note)
		if err != nil {
			writeCertError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, c)
	}
}

func tenantAndActor(w http.ResponseWriter, r *http.Request) (string, string) {
	tenant := mustTenant(w, r)
	if tenant == "" {
		return "", ""
	}
	actor := ""
	if pr, ok := principalFrom(r); ok {
		actor = pr.ID
	}
	return tenant, actor
}

func writeCertError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, certification.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{"not_found", err.Error()})
	default:
		writeJSON(w, http.StatusBadRequest, errorBody{"certification_error", err.Error()})
	}
}
