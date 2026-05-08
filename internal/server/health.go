package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"
)

// healthState tracks readiness. Liveness is always true once the process is up.
type healthState struct {
	ready   atomic.Bool
	started time.Time
}

func newHealthState() *healthState {
	return &healthState{started: time.Now()}
}

func (h *healthState) markReady()    { h.ready.Store(true) }
func (h *healthState) markNotReady() { h.ready.Store(false) }

// healthHandler builds the HTTP mux for /healthz and /readyz endpoints.
// Health endpoints are public and unauthenticated by design.
func healthHandler(h *healthState, version string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "ok",
			"version": version,
			"uptime":  time.Since(h.started).String(),
		})
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if !h.ready.Load() {
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "starting"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
	})
	return mux
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

// pingable narrows storage.Store to the only method this package uses, so
// ready checks can be wired without importing the full Store.
type pingable interface {
	Ping(ctx context.Context) error
}
