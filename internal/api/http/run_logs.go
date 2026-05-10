package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Satyaamm/plowered/internal/core/pipeline"
)

// runLogsHandlers registers two endpoints:
//
//	GET /v1/runs/{id}/logs?since=<id>&limit=<n>      — paginated read
//	GET /v1/runs/{id}/logs/stream?since=<id>         — SSE live tail
//
// Both first verify the caller can see the run (tenant match), then
// query the LogReader. The SSE endpoint polls every 500ms; switching to
// a Redis-stream backed pubsub is a future optimisation.
func runLogsHandlers(mux *http.ServeMux, runs pipeline.Repo, logs pipeline.LogReader) {
	mux.HandleFunc("GET /v1/runs/{id}/logs", listRunLogsHandler(runs, logs))
	mux.HandleFunc("GET /v1/runs/{id}/logs/stream", streamRunLogsHandler(runs, logs))
}

type logLineView struct {
	ID        int64     `json:"id"`
	TaskID    string    `json:"task_id,omitempty"`
	TaskRunID string    `json:"task_run_id,omitempty"`
	Level     string    `json:"level"`
	Line      string    `json:"line"`
	CreatedAt time.Time `json:"created_at"`
}

func toLogView(l pipeline.LogLine) logLineView {
	return logLineView{
		ID: l.ID, TaskID: l.TaskID, TaskRunID: l.TaskRunID,
		Level: l.Level, Line: l.Line, CreatedAt: l.CreatedAt,
	}
}

func listRunLogsHandler(runs pipeline.Repo, logs pipeline.LogReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		runID := r.PathValue("id")
		if !runVisibleToTenant(r.Context(), runs, runID, tenant) {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", "run not found"})
			return
		}
		since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		lines, err := logs.List(r.Context(), runID, since, limit)
		if err != nil {
			writeError(w, err)
			return
		}
		out := make([]logLineView, 0, len(lines))
		for _, l := range lines {
			out = append(out, toLogView(l))
		}
		writeJSON(w, http.StatusOK, map[string]any{"logs": out})
	}
}

// streamRunLogsHandler emits Server-Sent Events. Each line is a JSON
// payload on a `data:` field; the SSE last-event-id semantics are
// implemented via the `id:` field so a browser auto-reconnect resumes
// from the right cursor.
func streamRunLogsHandler(runs pipeline.Repo, logs pipeline.LogReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		runID := r.PathValue("id")
		if !runVisibleToTenant(r.Context(), runs, runID, tenant) {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", "run not found"})
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			writeJSON(w, http.StatusInternalServerError, errorBody{"sse_unsupported", "streaming not supported"})
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering
		w.WriteHeader(http.StatusOK)

		since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
		if lastEventID := r.Header.Get("Last-Event-ID"); lastEventID != "" {
			if v, err := strconv.ParseInt(lastEventID, 10, 64); err == nil {
				since = v
			}
		}

		ctx := r.Context()
		// Drain whatever's already there, then poll. Tail loop exits when
		// the run has hit a terminal state AND no new lines arrived in two
		// consecutive ticks — that's our "I think we're done" signal that
		// avoids hanging the connection forever on a stalled run.
		idleTicks := 0
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			lines, err := logs.List(ctx, runID, since, 200)
			if err != nil {
				_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
				flusher.Flush()
				return
			}
			for _, l := range lines {
				since = l.ID
				payload, _ := json.Marshal(toLogView(l))
				_, _ = fmt.Fprintf(w, "id: %d\ndata: %s\n\n", l.ID, payload)
			}
			if len(lines) > 0 {
				idleTicks = 0
				flusher.Flush()
			} else {
				idleTicks++
				// Heartbeat every 5s of silence keeps proxies from closing
				// the connection on idle.
				if idleTicks%10 == 0 {
					_, _ = fmt.Fprint(w, ": ping\n\n")
					flusher.Flush()
				}
			}
			// Stop tailing once the run is terminal AND we've drained
			// everything (idleTicks > 1 after a terminal state).
			if idleTicks > 1 {
				if isTerminal := runIsTerminal(ctx, runs, runID); isTerminal {
					_, _ = fmt.Fprint(w, "event: done\ndata: {}\n\n")
					flusher.Flush()
					return
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}
}

func runVisibleToTenant(ctx context.Context, runs pipeline.Repo, runID, tenant string) bool {
	r, err := runs.GetRun(ctx, runID)
	if err != nil || r == nil {
		return false
	}
	return r.TenantID == tenant
}

func runIsTerminal(ctx context.Context, runs pipeline.Repo, runID string) bool {
	r, err := runs.GetRun(ctx, runID)
	if err != nil || r == nil {
		return true
	}
	switch r.Status {
	case pipeline.RunSucceeded, pipeline.RunFailed, pipeline.RunCancelled:
		return true
	}
	return false
}
