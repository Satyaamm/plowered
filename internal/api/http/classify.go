package http

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/Satyaamm/plowered/internal/core/classifier"
	"github.com/Satyaamm/plowered/internal/core/jobs"
	"github.com/Satyaamm/plowered/internal/worker"
)

// Classifier is the small interface the HTTP layer needs from the
// classifier orchestrator. Decoupled so tests can substitute a stub.
type Classifier interface {
	ClassifyConnection(ctx context.Context, tenantID, connectionID, appliedBy string) (*classifier.Run, error)
	PreviewConnection(ctx context.Context, tenantID, connectionID string, opts classifier.PreviewOptions) (*classifier.Proposal, error)
	ApplyDecisions(ctx context.Context, tenantID, appliedBy string, decisions []classifier.Decision) (*classifier.ApplyResult, error)
	ListConnectionScope(ctx context.Context, tenantID, connectionID string) ([]classifier.TableRef, error)
}

// ClassificationReader powers /v1/assets/{id}/classifications.
type ClassificationReader interface {
	ListByAsset(ctx context.Context, tenantID, assetID string) ([]string, error)
}

func classifyHandlers(mux *http.ServeMux, c Classifier, reader ClassificationReader, jobsRepo jobs.Repo, enq worker.Enqueuer) {
	if c != nil {
		mux.HandleFunc("POST /v1/connections/{id}/classify", classifyConnectionHandler(c, jobsRepo, enq))
		mux.HandleFunc("POST /v1/connections/{id}/classify:preview", classifyPreviewHandler(c))
		mux.HandleFunc("POST /v1/connections/{id}/classify:apply", classifyApplyHandler(c))
		mux.HandleFunc("GET /v1/connections/{id}/scope", classifyScopeHandler(c))
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

// classifyPreviewRequest is the optional JSON body for /classify:preview.
// All fields are optional — an empty body previews every table.
type classifyPreviewRequest struct {
	Schemas []string `json:"schemas,omitempty"`
	Tables  []string `json:"tables,omitempty"`
}

// classifyPreviewHandler runs the sampler against the customer warehouse
// and returns proposed tags WITHOUT writing anything. Synchronous —
// the wizard polls this once on Step 2 entry.
func classifyPreviewHandler(c Classifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		connID := r.PathValue("id")

		var req classifyPreviewRequest
		if r.Body != nil && r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
				return
			}
		}

		ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
		defer cancel()
		proposal, err := c.PreviewConnection(ctx, tenant, connID, classifier.PreviewOptions{
			Schemas: req.Schemas,
			Tables:  req.Tables,
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody{"preview_failed", err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, proposalToJSON(proposal))
	}
}

// classifyApplyRequest is the JSON body for /classify:apply.
type classifyApplyRequest struct {
	Decisions []classifyApplyDecision `json:"decisions"`
}

type classifyApplyDecision struct {
	ColumnAssetID string   `json:"column_asset_id"`
	Tags          []string `json:"tags"`
}

// classifyApplyHandler writes the user-approved tags from the preview.
func classifyApplyHandler(c Classifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		actor := ""
		if pr, ok := principalFrom(r); ok {
			actor = pr.ID
		}

		var req classifyApplyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		decisions := make([]classifier.Decision, 0, len(req.Decisions))
		for _, d := range req.Decisions {
			decisions = append(decisions, classifier.Decision{
				ColumnAssetID: d.ColumnAssetID,
				Tags:          d.Tags,
			})
		}

		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		res, err := c.ApplyDecisions(ctx, tenant, actor, decisions)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody{"apply_failed", err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"applied":         res.Applied,
			"columns_updated": res.ColumnsUpdated,
		})
	}
}

// proposalToJSON shapes the orchestrator output for the wire. Field
// names are snake_case to match the rest of the v1 API; the lower
// orchestrator uses Go-idiomatic naming.
func proposalToJSON(p *classifier.Proposal) map[string]any {
	tables := make([]map[string]any, 0, len(p.Tables))
	for _, t := range p.Tables {
		cols := make([]map[string]any, 0, len(t.Columns))
		for _, c := range t.Columns {
			cols = append(cols, map[string]any{
				"asset_id":      c.AssetID,
				"name":          c.Name,
				"sampled":       c.Sampled,
				"hits":          c.Hits,
				"proposed_tags": c.ProposedTags,
			})
		}
		tables = append(tables, map[string]any{
			"asset_id": t.AssetID,
			"schema":   t.Schema,
			"name":     t.Name,
			"columns":  cols,
		})
	}
	skipped := make([]map[string]any, 0, len(p.Skipped))
	for _, s := range p.Skipped {
		skipped = append(skipped, map[string]any{
			"table":  s.Table,
			"reason": s.Reason,
		})
	}
	return map[string]any{
		"tables":  tables,
		"skipped": skipped,
	}
}

// classifyScopeHandler returns every (schema, table) the catalog knows
// about for this connection so the classify wizard can render real
// dropdowns instead of free-text inputs. Returns:
//
//	{"schemas":["public","analytics"],
//	 "tables":[{"schema":"public","name":"users","asset_id":"..."}]}
//
// Schemas is deduped and sorted. Tables are sorted (schema, name) for
// stable rendering.
func classifyScopeHandler(c Classifier) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		connID := r.PathValue("id")
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		refs, err := c.ListConnectionScope(ctx, tenant, connID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody{"scope_failed", err.Error()})
			return
		}
		schemaSet := map[string]struct{}{}
		tables := make([]map[string]any, 0, len(refs))
		for _, t := range refs {
			schemaSet[t.Schema] = struct{}{}
			tables = append(tables, map[string]any{
				"schema":   t.Schema,
				"name":     t.Name,
				"asset_id": t.AssetID,
			})
		}
		schemas := make([]string, 0, len(schemaSet))
		for s := range schemaSet {
			schemas = append(schemas, s)
		}
		sort.Strings(schemas)
		sort.Slice(tables, func(i, j int) bool {
			si, _ := tables[i]["schema"].(string)
			sj, _ := tables[j]["schema"].(string)
			if si != sj {
				return si < sj
			}
			ni, _ := tables[i]["name"].(string)
			nj, _ := tables[j]["name"].(string)
			return ni < nj
		})
		writeJSON(w, http.StatusOK, map[string]any{
			"schemas": schemas,
			"tables":  tables,
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
