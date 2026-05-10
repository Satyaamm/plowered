package http

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/Satyaamm/plowered/internal/core/jobs"
)

// jobsHandlers exposes the durable async-job ledger:
//
//	GET /v1/jobs/{id}   one job, full record (status/progress/result)
//	GET /v1/jobs        recent jobs for the tenant, newest first
//
// The frontend polls /v1/jobs/{id} every couple seconds while a long
// task is running; once status reaches succeeded/failed the result is
// final.
func jobsHandlers(mux *http.ServeMux, repo jobs.Repo) {
	mux.HandleFunc("GET /v1/jobs/{id}", getJobHandler(repo))
	mux.HandleFunc("GET /v1/jobs", listJobsHandler(repo))
}

type jobResp struct {
	ID          string         `json:"id"`
	Type        string         `json:"type"`
	Status      string         `json:"status"`
	ProgressPct int            `json:"progress_pct"`
	Message     string         `json:"message,omitempty"`
	Result      map[string]any `json:"result,omitempty"`
	Error       string         `json:"error,omitempty"`
	ResourceID  string         `json:"resource_id,omitempty"`
	ActorID     string         `json:"actor_id,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	StartedAt   *time.Time     `json:"started_at,omitempty"`
	FinishedAt  *time.Time     `json:"finished_at,omitempty"`
}

func toJobResp(j *jobs.Job) jobResp {
	r := jobResp{
		ID:          j.ID,
		Type:        j.Type,
		Status:      string(j.Status),
		ProgressPct: j.ProgressPct,
		Message:     j.Message,
		Result:      j.Result,
		Error:       j.ErrorMsg,
		ResourceID:  j.ResourceID,
		ActorID:     j.ActorID,
		CreatedAt:   j.CreatedAt,
	}
	if !j.StartedAt.IsZero() {
		t := j.StartedAt
		r.StartedAt = &t
	}
	if !j.FinishedAt.IsZero() {
		t := j.FinishedAt
		r.FinishedAt = &t
	}
	return r
}

func getJobHandler(repo jobs.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		j, err := repo.Get(r.Context(), tenant, r.PathValue("id"))
		if err != nil {
			if errors.Is(err, jobs.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, errorBody{"not_found", "job not found"})
				return
			}
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toJobResp(j))
	}
}

func listJobsHandler(repo jobs.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		limit := 50
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
				limit = n
			}
		}
		js, err := repo.List(r.Context(), tenant, limit)
		if err != nil {
			writeError(w, err)
			return
		}
		out := make([]jobResp, 0, len(js))
		for _, j := range js {
			out = append(out, toJobResp(j))
		}
		writeJSON(w, http.StatusOK, map[string]any{"jobs": out})
	}
}
