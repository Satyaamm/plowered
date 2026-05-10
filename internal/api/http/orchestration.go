package http

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Satyaamm/plowered/internal/core/auth"
	"github.com/Satyaamm/plowered/internal/core/deleted"
	"github.com/Satyaamm/plowered/internal/core/legalhold"
	"github.com/Satyaamm/plowered/internal/core/pipeline"
	"github.com/Satyaamm/plowered/internal/worker"
)

// pipelineHandlers wires pipeline + run endpoints onto mux.
func pipelineHandlers(mux *http.ServeMux, store pipeline.Repo, enq worker.Enqueuer, tomb deleted.Repo, holds legalhold.Repo) {
	mux.HandleFunc("GET /v1/pipelines",                listPipelinesHandler(store))
	mux.HandleFunc("POST /v1/pipelines",               createPipelineHandler(store))
	mux.HandleFunc("GET /v1/pipelines/{id}",           getPipelineHandler(store))
	mux.HandleFunc("PATCH /v1/pipelines/{id}",         updatePipelineHandler(store))
	mux.HandleFunc("DELETE /v1/pipelines/{id}",        deletePipelineHandler(store, tomb, holds))
	mux.HandleFunc("POST /v1/pipelines/{id}/trigger",  triggerPipelineHandler(store, enq))

	mux.HandleFunc("GET /v1/runs",                     listRunsHandler(store))
	mux.HandleFunc("GET /v1/runs/{id}",                getRunHandler(store))
	mux.HandleFunc("GET /v1/runs/{id}/tasks",          listTaskRunsHandler(store))
}

func listPipelinesHandler(s pipeline.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		out, err := s.ListPipelines(r.Context(), tenant)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"pipelines": out})
	}
}

func createPipelineHandler(s pipeline.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		var p pipeline.Pipeline
		if err := decodeJSON(r, &p); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if p.Name == "" {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "name required"})
			return
		}
		p.TenantID = tenant
		if pr, ok := principalFrom(r); ok {
			p.CreatedBy = pr.ID
			p.UpdatedBy = pr.ID
		}
		if _, err := pipeline.TopologicalSort(p.Tasks); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"invalid_argument", err.Error()})
			return
		}
		got, err := s.CreatePipeline(r.Context(), &p)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, got)
	}
}

func getPipelineHandler(s pipeline.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		got, err := s.GetPipeline(r.Context(), r.PathValue("id"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", err.Error()})
			return
		}
		if got.TenantID != tenant {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", "pipeline not found"})
			return
		}
		writeJSON(w, http.StatusOK, got)
	}
}

func updatePipelineHandler(s pipeline.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		id := r.PathValue("id")
		existing, err := s.GetPipeline(r.Context(), id)
		if err != nil || existing.TenantID != tenant {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", "pipeline not found"})
			return
		}
		var patch pipeline.Pipeline
		if err := decodeJSON(r, &patch); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		patch.ID = id
		patch.TenantID = tenant
		patch.CreatedAt = existing.CreatedAt
		patch.CreatedBy = existing.CreatedBy
		if pr, ok := principalFrom(r); ok {
			patch.UpdatedBy = pr.ID
		}
		if _, err := pipeline.TopologicalSort(patch.Tasks); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"invalid_argument", err.Error()})
			return
		}
		got, err := s.UpdatePipeline(r.Context(), &patch)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, got)
	}
}

func deletePipelineHandler(s pipeline.Repo, tomb deleted.Repo, holds legalhold.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		id := r.PathValue("id")
		// Capture the row into the recycle bin BEFORE deleting it so a
		// failure mid-flow leaves a tombstone the user can restore from.
		existing, err := s.GetPipeline(r.Context(), id)
		if err != nil || existing.TenantID != tenant {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", "pipeline not found"})
			return
		}
		if tomb != nil {
			if err := captureTombstone(r, tomb, holds, "pipeline", existing.ID, existing); err != nil {
				writeDeleteError(w, err)
				return
			}
		}
		if err := s.DeletePipeline(r.Context(), tenant, id); err != nil {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// pipelineRestorer pulls the JSON payload out of a tombstone and re-inserts
// the pipeline with its original ID. Children (runs, task_runs) are not
// reinstated — they belong to a different point in time and operators
// rerun rather than restore.
func pipelineRestorer(s pipeline.Repo) Restorer {
	return func(ctx context.Context, rec *deleted.Record) error {
		var p pipeline.Pipeline
		if err := remapJSON(rec.Payload, &p); err != nil {
			return err
		}
		_, err := s.CreatePipeline(ctx, &p)
		return err
	}
}

// triggerPipelineHandler enqueues a Run for an existing pipeline. The
// handler persists the queued state and dispatches an async job; the
// worker tier picks it up and drives it to completion.
func triggerPipelineHandler(s pipeline.Repo, enq worker.Enqueuer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		pid := r.PathValue("id")
		p, err := s.GetPipeline(r.Context(), pid)
		if err != nil || p.TenantID != tenant {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", "pipeline not found"})
			return
		}
		triggeredBy := "api"
		if pr, ok := principalFrom(r); ok {
			triggeredBy = pr.ID
		}
		// Mint an idempotency key — the table has UNIQUE(tenant, pipeline,
		// idempotency_key) and the column is NOT NULL, so two manual triggers
		// for the same pipeline would otherwise collide. Using nano-second
		// precision plus the request ID keeps that effectively unique even
		// under heavy concurrency from one operator.
		idem := fmt.Sprintf("manual-%d", time.Now().UnixNano())
		run, err := s.CreateRun(r.Context(), &pipeline.Run{
			TenantID:       tenant,
			PipelineID:     pid,
			Status:         pipeline.RunQueued,
			ScheduledAt:    time.Now().UTC(),
			TriggeredBy:    triggeredBy,
			IdempotencyKey: idem,
		})
		if err != nil {
			writeError(w, err)
			return
		}
		// Best-effort enqueue: a queue outage shouldn't block trigger; the
		// stuck-run reaper (batch 4) will surface neglected runs.
		_ = enq.EnqueuePipelineRun(r.Context(), worker.PipelineRunPayload{
			TenantID: tenant, RunID: run.ID,
		})
		writeJSON(w, http.StatusAccepted, run)
	}
}

func listRunsHandler(s pipeline.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		out, err := s.ListRuns(r.Context(), tenant, q.Get("pipeline_id"), limit)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"runs": out})
	}
}

func getRunHandler(s pipeline.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		got, err := s.GetRun(r.Context(), r.PathValue("id"))
		if err != nil || got.TenantID != tenant {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", "run not found"})
			return
		}
		writeJSON(w, http.StatusOK, got)
	}
}

func listTaskRunsHandler(s pipeline.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		runID := r.PathValue("id")
		run, err := s.GetRun(r.Context(), runID)
		if err != nil || run.TenantID != tenant {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", "run not found"})
			return
		}
		trs, err := s.ListTaskRuns(r.Context(), runID)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"task_runs": trs})
	}
}

// ----- shared helpers -----

func mustTenant(w http.ResponseWriter, r *http.Request) string {
	p, err := auth.PrincipalFromContext(r.Context())
	if err != nil || p.TenantID == "" {
		writeJSON(w, http.StatusUnauthorized, errorBody{"tenant_required", "tenant_id missing"})
		return ""
	}
	return p.TenantID
}

func principalFrom(r *http.Request) (auth.Principal, bool) {
	p, err := auth.PrincipalFromContext(r.Context())
	if err != nil {
		return auth.Principal{}, false
	}
	return p, true
}
