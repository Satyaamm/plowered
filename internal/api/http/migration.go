package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/Satyaamm/plowered/internal/core/migration"
)

// Migrator is the small surface the HTTP layer needs. Decoupled from
// *migration.Service so handler tests can stub it.
type Migrator interface {
	CreatePlan(ctx context.Context, p *migration.Plan) (*migration.Plan, error)
	GetPlan(ctx context.Context, tenantID, planID string) (*migration.Plan, error)
	ListPlans(ctx context.Context, tenantID string) ([]*migration.Plan, error)
	DeletePlan(ctx context.Context, tenantID, planID string) error
	RunPlan(ctx context.Context, tenantID, planID string) (*migration.Run, error)
	ListRuns(ctx context.Context, tenantID, planID string) ([]*migration.Run, error)
}

func migrationHandlers(mux *http.ServeMux, m Migrator) {
	if m == nil {
		return
	}
	mux.HandleFunc("GET    /v1/migrations",              listPlansHandler(m))
	mux.HandleFunc("POST   /v1/migrations",              createPlanHandler(m))
	mux.HandleFunc("GET    /v1/migrations/{id}",         getPlanHandler(m))
	mux.HandleFunc("DELETE /v1/migrations/{id}",         deletePlanHandler(m))
	mux.HandleFunc("POST   /v1/migrations/{id}/run",     runPlanHandler(m))
	mux.HandleFunc("GET    /v1/migrations/{id}/runs",    listMigRunsHandler(m))
}

func listPlansHandler(m Migrator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		plans, err := m.ListPlans(r.Context(), tenant)
		if err != nil {
			writeMigError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"plans": plans})
	}
}

type createPlanReq struct {
	Name               string                 `json:"name"`
	SourceConnectionID string                 `json:"source_connection_id"`
	SourceSchema       string                 `json:"source_schema"`
	SourceTable        string                 `json:"source_table"`
	DestConnectionID   string                 `json:"dest_connection_id"`
	DestSchema         string                 `json:"dest_schema"`
	DestTable          string                 `json:"dest_table"`
	ColumnMap          []migration.ColumnMap  `json:"column_map"`
	Mode               migration.Mode         `json:"mode"`
	WriteMode          migration.WriteMode    `json:"write_mode"`
}

func createPlanHandler(m Migrator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		actor := ""
		if pr, ok := principalFrom(r); ok {
			actor = pr.ID
		}
		var req createPlanReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		plan := &migration.Plan{
			TenantID:           tenant,
			Name:               req.Name,
			SourceConnectionID: req.SourceConnectionID,
			SourceSchema:       req.SourceSchema,
			SourceTable:        req.SourceTable,
			DestConnectionID:   req.DestConnectionID,
			DestSchema:         req.DestSchema,
			DestTable:          req.DestTable,
			ColumnMap:          req.ColumnMap,
			Mode:               req.Mode,
			WriteMode:          req.WriteMode,
			CreatedBy:          actor,
		}
		out, err := m.CreatePlan(r.Context(), plan)
		if err != nil {
			writeMigError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, out)
	}
}

func getPlanHandler(m Migrator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		p, err := m.GetPlan(r.Context(), tenant, r.PathValue("id"))
		if err != nil {
			writeMigError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, p)
	}
}

func deletePlanHandler(m Migrator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		if err := m.DeletePlan(r.Context(), tenant, r.PathValue("id")); err != nil {
			writeMigError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// runPlanHandler is synchronous for v0 — the executor caps at 30
// minutes and we'd rather wait than fire-and-forget. Async via a jobs
// queue is a follow-up.
func runPlanHandler(m Migrator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 35*time.Minute)
		defer cancel()
		run, err := m.RunPlan(ctx, tenant, r.PathValue("id"))
		if err != nil {
			// Even on failure we want to ship the Run row so the UI
			// can render status=failed + the error string.
			if run != nil {
				writeJSON(w, http.StatusOK, run)
				return
			}
			writeMigError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, run)
	}
}

func listMigRunsHandler(m Migrator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		runs, err := m.ListRuns(r.Context(), tenant, r.PathValue("id"))
		if err != nil {
			writeMigError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
	}
}

func writeMigError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, migration.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{"not_found", err.Error()})
	case errors.Is(err, migration.ErrModeUnimplemented):
		writeJSON(w, http.StatusBadRequest, errorBody{"mode_unimplemented", err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, errorBody{"migration_failed", err.Error()})
	}
}
