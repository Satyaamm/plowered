package http

import (
	"net/http"
	"time"

	"github.com/Satyaamm/plowered/internal/core/auth"
	"github.com/Satyaamm/plowered/internal/core/dsr"
	"github.com/Satyaamm/plowered/internal/core/policy"
)

// dsrHandlers exposes GDPR Art.15-20 endpoints. Subjects (or their reps)
// file requests via POST /v1/dsr; tenant operators see + advance them via
// the GET/PATCH endpoints.
//
// The work itself — building the access/portability bundle, performing
// the erasure — happens out-of-band: a worker reads received requests
// and writes back artifact_urn + status. We expose the queue here.
func dsrHandlers(mux *http.ServeMux, repo dsr.Repo) {
	mux.HandleFunc("POST /v1/dsr",                 createDSRHandler(repo))
	mux.HandleFunc("GET /v1/dsr",                  listDSRHandler(repo))
	mux.HandleFunc("GET /v1/dsr/{id}",             getDSRHandler(repo))
	mux.HandleFunc("PATCH /v1/dsr/{id}/status",    updateDSRStatusHandler(repo))
}

// createDSRRequest is the POST body. We accept just enough to start the
// 30-day clock — the worker fills in the rest.
type createDSRRequest struct {
	SubjectID string `json:"subject_id"`
	Type      string `json:"type"`
	Notes     string `json:"notes,omitempty"`
}

func createDSRHandler(repo dsr.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		var body createDSRRequest
		if err := decodeJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if body.SubjectID == "" || body.Type == "" {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request",
				"subject_id and type are required"})
			return
		}
		if !validDSRType(body.Type) {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request",
				"type must be one of access|portability|rectification|erasure|restriction"})
			return
		}
		out, err := repo.Create(r.Context(), &dsr.Request{
			TenantID:  tenant,
			SubjectID: body.SubjectID,
			Type:      dsr.Type(body.Type),
			Notes:     body.Notes,
		})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, out)
	}
}

func listDSRHandler(repo dsr.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		out, err := repo.List(r.Context(), tenant)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"requests": out})
	}
}

func getDSRHandler(repo dsr.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		got, err := repo.Get(r.Context(), tenant, r.PathValue("id"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, got)
	}
}

// updateDSRStatusRequest advances a DSR through its lifecycle.
type updateDSRStatusRequest struct {
	Status      string `json:"status"`
	ArtifactURN string `json:"artifact_urn,omitempty"`
	Notes       string `json:"notes,omitempty"`
}

func updateDSRStatusHandler(repo dsr.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		p, _ := auth.PrincipalFromContext(r.Context())
		// Status changes are an operator action — DPO/admin only.
		if !policy.HasRole(p, "admin") && !policy.HasRole(p, "super_admin") {
			writeJSON(w, http.StatusForbidden, errorBody{"forbidden",
				"only admin/super_admin can change DSR status"})
			return
		}
		var body updateDSRStatusRequest
		if err := decodeJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if !validDSRStatus(body.Status) {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request",
				"status must be one of received|processing|completed|rejected"})
			return
		}
		if err := repo.UpdateStatus(r.Context(), tenant, r.PathValue("id"),
			dsr.Status(body.Status), p.ID, body.ArtifactURN, body.Notes); err != nil {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", err.Error()})
			return
		}
		got, _ := repo.Get(r.Context(), tenant, r.PathValue("id"))
		writeJSON(w, http.StatusOK, got)
	}
}

func validDSRType(s string) bool {
	switch dsr.Type(s) {
	case dsr.TypeAccess, dsr.TypePortability, dsr.TypeRectification, dsr.TypeErasure, dsr.TypeRestriction:
		return true
	}
	return false
}

func validDSRStatus(s string) bool {
	switch dsr.Status(s) {
	case dsr.StatusReceived, dsr.StatusProcessing, dsr.StatusCompleted, dsr.StatusRejected:
		return true
	}
	return false
}

// _ is kept to silence unused-import linters during early development of
// the worker side — drop once a runtime call references time. (used by
// dsr.SLA constant test wiring.)
var _ = time.Now
