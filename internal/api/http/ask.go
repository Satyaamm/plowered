package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/Satyaamm/plowered/internal/core/aiprovider"
	"github.com/Satyaamm/plowered/internal/core/asker"
	"github.com/Satyaamm/plowered/internal/core/policy"
)

// Asker is the small surface the HTTP layer needs.
type Asker interface {
	Ask(ctx context.Context, tenantID, connectionID, question, generatedBy string) (*asker.Generation, error)
	Run(ctx context.Context, tenantID, executionID string) (*asker.RunResult, error)
	History(ctx context.Context, tenantID string, limit int) ([]*asker.Execution, error)
}

func askHandlers(mux *http.ServeMux, a Asker, authz policy.Authorizer) {
	if a == nil {
		return
	}
	mux.HandleFunc("POST /v1/ai:ask", askGenerateHandler(a, authz))
	mux.HandleFunc("POST /v1/ai:ask/{id}/run", askRunHandler(a, authz))
	mux.HandleFunc("GET /v1/ai:ask/history", askHistoryHandler(a, authz))
}

func askHistoryHandler(a Asker, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := gateTenantAndVerb(w, r, authz, policy.VerbRead, "ai_query")
		if tenant == "" {
			return
		}
		limit := 100
		if s := r.URL.Query().Get("limit"); s != "" {
			if n, err := strconvAtoi(s); err == nil && n > 0 {
				limit = n
			}
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		execs, err := a.History(ctx, tenant, limit)
		if err != nil {
			writeAskError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"executions": execs})
	}
}

// strconvAtoi is inlined here to avoid an extra import on a one-shot
// parse; the handler accepts ?limit=N from the query string.
func strconvAtoi(s string) (int, error) {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errors.New("bad int")
		}
		n = n*10 + int(c-'0')
	}
	return n, nil
}

type askRequest struct {
	ConnectionID string `json:"connection_id"`
	Question     string `json:"question"`
}

func askGenerateHandler(a Asker, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Drafting SQL burns LLM tokens + warehouse seconds on Run.
		// VerbRun gates against the role matrix (editor+).
		tenant := gateTenantAndVerb(w, r, authz, policy.VerbRun, "ai_query")
		if tenant == "" {
			return
		}
		actor := ""
		if pr, ok := principalFrom(r); ok {
			actor = pr.ID
		}
		var req askRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if req.ConnectionID == "" || req.Question == "" {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "connection_id + question required"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		gen, err := a.Ask(ctx, tenant, req.ConnectionID, req.Question, actor)
		if err != nil {
			writeAskError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, gen)
	}
}

func askRunHandler(a Asker, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := gateTenantAndVerb(w, r, authz, policy.VerbRun, "ai_query")
		if tenant == "" {
			return
		}
		// 120s — running an LLM-produced query can be a 60s table scan
		// on a customer warehouse; we'd rather wait than 504. The
		// service caps rows separately.
		ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
		defer cancel()
		res, err := a.Run(ctx, tenant, r.PathValue("id"))
		if err != nil {
			writeAskError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, res)
	}
}

func writeAskError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, aiprovider.ErrNoPrimary):
		writeJSON(w, http.StatusBadRequest, errorBody{
			"no_ai_provider",
			"No primary chat provider configured. Add one in Settings → AI providers.",
		})
	case errors.Is(err, asker.ErrUnsafeSQL):
		writeJSON(w, http.StatusBadRequest, errorBody{
			"unsafe_sql",
			err.Error(),
		})
	default:
		writeJSON(w, http.StatusInternalServerError, errorBody{
			"ask_failed",
			err.Error(),
		})
	}
}
