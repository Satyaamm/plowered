package http

import (
	"context"
	"net/http"
	"strconv"

	"github.com/Satyaamm/plowered/internal/core/deleted"
	"github.com/Satyaamm/plowered/internal/core/legalhold"
	"github.com/Satyaamm/plowered/internal/core/quality"
	"github.com/Satyaamm/plowered/internal/worker"
)

func checkHandlers(mux *http.ServeMux, store quality.Store, enq worker.Enqueuer, tomb deleted.Repo, holds legalhold.Repo) {
	mux.HandleFunc("GET /v1/checks",                listChecksHandler(store))
	mux.HandleFunc("POST /v1/checks",               createCheckHandler(store))
	mux.HandleFunc("GET /v1/checks/{id}",           getCheckHandler(store))
	mux.HandleFunc("PATCH /v1/checks/{id}",         updateCheckHandler(store))
	mux.HandleFunc("DELETE /v1/checks/{id}",        deleteCheckHandler(store, tomb, holds))
	mux.HandleFunc("GET /v1/checks/{id}/runs",      listCheckRunsHandler(store))
	mux.HandleFunc("POST /v1/checks/{id}/run",      runCheckHandler(store, enq))
}

// runCheckRequest is the optional body posted to /v1/checks/{id}/run.
type runCheckRequest struct {
	SourceID      string  `json:"source_id"`
	SamplePercent float64 `json:"sample_percent,omitempty"`
	TimeoutSec    int     `json:"timeout_sec,omitempty"`
}

func runCheckHandler(s quality.Store, enq worker.Enqueuer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		id := r.PathValue("id")
		c, err := s.GetCheck(r.Context(), id)
		if err != nil || c.TenantID != tenant {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", "check not found"})
			return
		}
		var req runCheckRequest
		if r.ContentLength > 0 {
			if err := decodeJSON(r, &req); err != nil {
				writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
				return
			}
		}
		if err := enq.EnqueueQualityRun(r.Context(), worker.QualityRunPayload{
			TenantID:      tenant,
			CheckID:       id,
			SourceID:      req.SourceID,
			SamplePercent: req.SamplePercent,
			TimeoutSec:    req.TimeoutSec,
		}); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "queued", "check_id": id})
	}
}

func listChecksHandler(s quality.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		out, err := s.ListChecks(r.Context(), tenant, r.URL.Query().Get("asset_id"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"checks": out})
	}
}

func createCheckHandler(s quality.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		var c quality.Check
		if err := decodeJSON(r, &c); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if c.Name == "" || c.Type == "" || c.AssetID == "" {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "name, type, asset_id required"})
			return
		}
		c.TenantID = tenant
		got, err := s.CreateCheck(r.Context(), &c)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, got)
	}
}

func getCheckHandler(s quality.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		got, err := s.GetCheck(r.Context(), r.PathValue("id"))
		if err != nil || got.TenantID != tenant {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", "check not found"})
			return
		}
		writeJSON(w, http.StatusOK, got)
	}
}

func updateCheckHandler(s quality.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		id := r.PathValue("id")
		existing, err := s.GetCheck(r.Context(), id)
		if err != nil || existing.TenantID != tenant {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", "check not found"})
			return
		}
		var patch quality.Check
		if err := decodeJSON(r, &patch); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		patch.ID = id
		patch.TenantID = tenant
		patch.CreatedAt = existing.CreatedAt
		got, err := s.UpdateCheck(r.Context(), &patch)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, got)
	}
}

func deleteCheckHandler(s quality.Store, tomb deleted.Repo, holds legalhold.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		id := r.PathValue("id")
		existing, err := s.GetCheck(r.Context(), id)
		if err != nil || existing.TenantID != tenant {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", "check not found"})
			return
		}
		if tomb != nil {
			if err := captureTombstone(r, tomb, holds, "check", existing.ID, existing); err != nil {
				writeDeleteError(w, err)
				return
			}
		}
		if err := s.DeleteCheck(r.Context(), tenant, id); err != nil {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func checkRestorer(s quality.Store) Restorer {
	return func(ctx context.Context, rec *deleted.Record) error {
		var c quality.Check
		if err := remapJSON(rec.Payload, &c); err != nil {
			return err
		}
		_, err := s.CreateCheck(ctx, &c)
		return err
	}
}

func listCheckRunsHandler(s quality.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		id := r.PathValue("id")
		c, err := s.GetCheck(r.Context(), id)
		if err != nil || c.TenantID != tenant {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", "check not found"})
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		out, err := s.ListRuns(r.Context(), tenant, id, limit)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"runs": out})
	}
}
