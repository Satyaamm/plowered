package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Satyaamm/plowered/internal/core/migration"
	"github.com/Satyaamm/plowered/internal/core/policy"
	"github.com/Satyaamm/plowered/internal/worker"
)

// Migrator is the small surface the HTTP layer needs. Decoupled from
// *migration.Service so handler tests can stub it.
type Migrator interface {
	CreatePlan(ctx context.Context, p *migration.Plan) (*migration.Plan, error)
	GetPlan(ctx context.Context, tenantID, planID string) (*migration.Plan, error)
	ListPlans(ctx context.Context, tenantID string) ([]*migration.Plan, error)
	DeletePlan(ctx context.Context, tenantID, planID string) error
	// EnqueueRun creates the running-status Run row and returns it. The
	// HTTP layer pairs this with a worker.Enqueuer call to dispatch the
	// actual work asynchronously.
	EnqueueRun(ctx context.Context, tenantID, planID string) (*migration.Run, error)
	ListRuns(ctx context.Context, tenantID, planID string) ([]*migration.Run, error)
}

func migrationHandlers(mux *http.ServeMux, m Migrator, enq worker.Enqueuer, authz policy.Authorizer) {
	if m == nil {
		return
	}
	mux.HandleFunc("GET    /v1/migrations",              listPlansHandler(m, authz))
	mux.HandleFunc("POST   /v1/migrations",              createPlanHandler(m, authz))
	mux.HandleFunc("GET    /v1/migrations/{id}",         getPlanHandler(m, authz))
	mux.HandleFunc("DELETE /v1/migrations/{id}",         deletePlanHandler(m, authz))
	mux.HandleFunc("POST   /v1/migrations/{id}/run",     runPlanHandler(m, enq, authz))
	mux.HandleFunc("GET    /v1/migrations/{id}/runs",    listMigRunsHandler(m, authz))
}

func listPlansHandler(m Migrator, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := gateTenantAndVerb(w, r, authz, policy.VerbRead, "migration")
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

func createPlanHandler(m Migrator, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant, actor := gateTenantActorAndVerb(w, r, authz, policy.VerbEdit, "migration")
		if tenant == "" {
			return
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

func getPlanHandler(m Migrator, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := gateTenantAndVerb(w, r, authz, policy.VerbRead, "migration")
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

func deletePlanHandler(m Migrator, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := gateTenantAndVerb(w, r, authz, policy.VerbDelete, "migration")
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

// runPlanHandler creates a running-status Run row and hands the work
// off to the worker. Returns 202 with the in-flight Run so the UI can
// start polling /runs immediately. If no enqueuer is wired (the noop
// fallback) the row is still created — useful for tests, less useful in
// production, so main.go is expected to wire a real enqueuer.
func runPlanHandler(m Migrator, enq worker.Enqueuer, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Running a migration in prod touches customer warehouses with
		// credentials — admin-only by default; per-asset rules can widen.
		tenant := gateTenantAndVerb(w, r, authz, policy.VerbAdmin, "migration")
		if tenant == "" {
			return
		}
		planID := r.PathValue("id")
		run, err := m.EnqueueRun(r.Context(), tenant, planID)
		if err != nil {
			writeMigError(w, err)
			return
		}
		if enq != nil {
			if err := enq.EnqueueMigrationRun(r.Context(), worker.MigrationRunPayload{
				TenantID: tenant,
				PlanID:   planID,
				RunID:    run.ID,
			}); err != nil {
				// The row exists; surface the dispatch failure so the
				// caller knows the worker won't pick it up.
				writeJSON(w, http.StatusInternalServerError, errorBody{"enqueue_failed", err.Error()})
				return
			}
		}
		writeJSON(w, http.StatusAccepted, run)
	}
}

func listMigRunsHandler(m Migrator, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := gateTenantAndVerb(w, r, authz, policy.VerbRead, "migration")
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
