package http

import (
	"context"
	"net/http"
	"time"

	"github.com/Satyaamm/plowered/internal/core/classifier"
	"github.com/Satyaamm/plowered/internal/core/jobs"
	"github.com/Satyaamm/plowered/internal/worker"
)

// Classifier is the small interface the HTTP layer needs from the
// classifier orchestrator. Decoupled so tests can substitute a stub.
type Classifier interface {
	ClassifyConnection(ctx context.Context, tenantID, connectionID, appliedBy string) (*classifier.Run, error)
}

// ClassificationReader powers /v1/assets/{id}/classifications.
type ClassificationReader interface {
	ListByAsset(ctx context.Context, tenantID, assetID string) ([]string, error)
}

func classifyHandlers(mux *http.ServeMux, c Classifier, reader ClassificationReader, jobsRepo jobs.Repo, enq worker.Enqueuer) {
	if c != nil {
		mux.HandleFunc("POST /v1/connections/{id}/classify", classifyConnectionHandler(c, jobsRepo, enq))
	}
	if reader != nil {
		mux.HandleFunc("GET /v1/assets/{id}/classifications", listAssetClassificationsHandler(reader))
	}
}

// classifyConnectionHandler enqueues a classification job when a Jobs
// repo + Enqueuer are wired (the production path) and returns
// 202 + {job_id}. When neither is set the handler falls back to the
// legacy synchronous run with a 90s budget.
func classifyConnectionHandler(c Classifier, jobsRepo jobs.Repo, enq worker.Enqueuer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		actor := ""
		if pr, ok := principalFrom(r); ok {
			actor = pr.ID
		}
		connID := r.PathValue("id")

		if jobsRepo != nil && enq != nil {
			job, err := jobsRepo.Create(r.Context(), &jobs.Job{
				TenantID:   tenant,
				Type:       jobs.TypeClassifyConnection,
				ActorID:    actor,
				ResourceID: connID,
			})
			if err != nil {
				writeError(w, err)
				return
			}
			if err := enq.EnqueueClassifyConnection(r.Context(), worker.ClassifyConnectionPayload{
				TenantID:     tenant,
				ConnectionID: connID,
				Actor:        actor,
				JobID:        job.ID,
			}); err != nil {
				_ = jobsRepo.Fail(r.Context(), job.ID, err.Error())
				writeJSON(w, http.StatusInternalServerError, errorBody{"enqueue_failed", err.Error()})
				return
			}
			writeJSON(w, http.StatusAccepted, map[string]any{
				"job_id":      job.ID,
				"status":      string(job.Status),
				"resource_id": connID,
			})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		run, err := c.ClassifyConnection(ctx, tenant, connID, actor)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody{"classify_failed", err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"tables":  run.Tables,
			"columns": run.Columns,
			"tagged":  run.Tagged,
			"skipped": run.Skipped,
		})
	}
}

func listAssetClassificationsHandler(reader ClassificationReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		tags, err := reader.ListByAsset(r.Context(), tenant, r.PathValue("id"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"classifications": tags})
	}
}
