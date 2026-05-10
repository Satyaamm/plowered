package http

import (
	"context"
	"net/http"

	"github.com/Satyaamm/plowered/internal/core/auth"
	"github.com/Satyaamm/plowered/internal/core/graph"
	"github.com/Satyaamm/plowered/internal/core/jobs"
	"github.com/Satyaamm/plowered/internal/core/policy"
	"github.com/Satyaamm/plowered/internal/core/search"
	"github.com/Satyaamm/plowered/internal/worker"
)

// semanticHandlers registers the embedding-driven search surface:
//
//	POST /v1/search:semantic   body: {query, k}
//	POST /v1/search:reindex    walks the catalog and refreshes vectors
//
// Search results are filtered through the policy engine so an LLM agent
// (or any user) cannot bypass tag-based deny rules just by phrasing
// their question creatively.
func semanticHandlers(mux *http.ServeMux, ix *search.Indexer, srch *search.Searcher, rules policy.RuleStore, jobsRepo jobs.Repo, enq worker.Enqueuer) {
	mux.HandleFunc("POST /v1/search:semantic", semanticSearchHandler(srch, rules))
	mux.HandleFunc("POST /v1/search:reindex", reindexHandler(ix, jobsRepo, enq))
}

type semanticRequest struct {
	Query string `json:"query"`
	K     int    `json:"k"`
}

type semanticHit struct {
	AssetID       string   `json:"asset_id"`
	QualifiedName string   `json:"qualified_name"`
	Type          string   `json:"type"`
	Description   string   `json:"description,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	Score         float32  `json:"score"`
}

func semanticSearchHandler(srch *search.Searcher, rules policy.RuleStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		var req semanticRequest
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if req.Query == "" {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "query is required"})
			return
		}

		// Inject a per-request policy filter that uses the caller's principal.
		// We don't mutate Searcher — every request gets its own clone-with-filter.
		filtered := *srch
		filtered.Filter = policyFilter(rules, principalForRequest(r))

		hits, err := filtered.Query(r.Context(), tenant, req.Query, req.K)
		if err != nil {
			writeError(w, err)
			return
		}
		out := make([]semanticHit, 0, len(hits))
		for _, h := range hits {
			out = append(out, semanticHit{
				AssetID:       h.Asset.ID,
				QualifiedName: h.Asset.QualifiedName,
				Type:          string(h.Asset.Type),
				Description:   h.Asset.Description,
				Tags:          h.Asset.Tags,
				Score:         h.Score,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"query": req.Query,
			"hits":  out,
		})
	}
}

// reindexHandler enqueues a search:reindex job when Jobs + Enqueuer are
// configured (the production path) and returns 202 + {job_id}. Without
// them it falls back to the synchronous IndexAll, which still works for
// small embedded deployments.
func reindexHandler(ix *search.Indexer, jobsRepo jobs.Repo, enq worker.Enqueuer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		actor := ""
		if pr, ok := principalFrom(r); ok {
			actor = pr.ID
		}
		if jobsRepo != nil && enq != nil {
			job, err := jobsRepo.Create(r.Context(), &jobs.Job{
				TenantID: tenant,
				Type:     jobs.TypeSearchReindex,
				ActorID:  actor,
			})
			if err != nil {
				writeError(w, err)
				return
			}
			if err := enq.EnqueueSearchReindex(r.Context(), worker.SearchReindexPayload{
				TenantID: tenant,
				Actor:    actor,
				JobID:    job.ID,
			}); err != nil {
				_ = jobsRepo.Fail(r.Context(), job.ID, err.Error())
				writeJSON(w, http.StatusInternalServerError, errorBody{"enqueue_failed", err.Error()})
				return
			}
			writeJSON(w, http.StatusAccepted, map[string]any{
				"job_id": job.ID,
				"status": string(job.Status),
				"model":  ix.Provider.Name(),
			})
			return
		}
		written, err := ix.IndexAll(r.Context(), tenant)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"reindexed": written,
			"model":     ix.Provider.Name(),
		})
	}
}

func principalForRequest(r *http.Request) auth.Principal {
	p, _ := auth.PrincipalFromContext(r.Context())
	return p
}

func policyFilter(rules policy.RuleStore, p auth.Principal) search.Filter {
	if rules == nil {
		return nil
	}
	engine := policy.NewEngine(rules)
	return func(ctx context.Context, a *graph.Asset) bool {
		dec := engine.Allow(ctx, p, policy.VerbRead, policy.Resource{
			Type:     "asset",
			ID:       a.ID,
			TenantID: a.TenantID,
			Tags:     a.Tags,
			OwnerIDs: a.Owners,
		})
		return dec.Allow
	}
}
