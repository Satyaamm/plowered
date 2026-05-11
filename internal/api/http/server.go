// Package http exposes Plowered's catalog over a JSON REST API. It is the
// surface the web UI and any third-party integrations talk to today; the
// proto-defined gRPC surface fills in alongside once `buf generate` runs.
//
// All handlers run behind the same auth + tenant + audit chain as the gRPC
// server (see internal/api/middleware), translated to net/http.
package http

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Satyaamm/plowered/internal/core/aiprovider"
	"github.com/Satyaamm/plowered/internal/core/audit"
	"github.com/Satyaamm/plowered/internal/core/connection"
	"github.com/Satyaamm/plowered/internal/core/deleted"
	"github.com/Satyaamm/plowered/internal/core/dsr"
	"github.com/Satyaamm/plowered/internal/core/email"
	"github.com/Satyaamm/plowered/internal/core/glossary"
	"github.com/Satyaamm/plowered/internal/core/graph"
	"github.com/Satyaamm/plowered/internal/core/identity"
	"github.com/Satyaamm/plowered/internal/core/jobs"
	"github.com/Satyaamm/plowered/internal/core/legalhold"
	"github.com/Satyaamm/plowered/internal/core/notify"
	"github.com/Satyaamm/plowered/internal/core/secrets"
	"github.com/Satyaamm/plowered/internal/core/pipeline"
	"github.com/Satyaamm/plowered/internal/core/policy"
	"github.com/Satyaamm/plowered/internal/core/quality"
	"github.com/Satyaamm/plowered/internal/core/search"
	"github.com/Satyaamm/plowered/internal/storage"
	"github.com/Satyaamm/plowered/internal/worker"
)

// Deps bundles the stores and services the HTTP layer needs. Catalog is
// required; the rest are optional — pass nil to skip registering those
// routes.
type Deps struct {
	Catalog   storage.Store
	Pipelines pipeline.Repo
	Quality   quality.Store
	Notify    notify.Repo
	Policies  policy.RuleRepo
	Audit       audit.Reader
	AuditWriter audit.Writer
	Deleted     deleted.Repo
	LegalHolds  legalhold.Repo
	DSR         dsr.Repo
	Identity    identity.Repo
	Email       email.Sender
	AuthCfg     AuthConfig
	Connections connection.Repo
	ConnRegistry *connection.Registry
	Vault       secrets.Vault

	// Enqueuer dispatches async jobs (pipeline runs, quality checks). When
	// nil, NewMux falls back to worker.NoopEnqueuer — handlers still respond
	// quickly but no background work happens.
	Enqueuer worker.Enqueuer

	// Logs powers the /v1/runs/{id}/logs read + SSE tail endpoint. Optional;
	// when nil the routes return 404.
	Logs pipeline.LogReader

	// ColumnLineage powers /v1/assets/{id}/column-lineage. Optional.
	ColumnLineage ColumnLineageReader

	// Glossary powers /v1/glossary/* and the term assignment endpoints.
	Glossary glossary.Repo

	// Classifier runs sample-based classification jobs. Optional.
	Classifier         Classifier
	Classifications    ClassificationReader

	// Indexer + Searcher power /v1/search:semantic. Optional.
	SearchIndexer  *search.Indexer
	SearchSearcher *search.Searcher

	// Jobs powers /v1/jobs/{id} polling and tracks long-running async
	// work (classify, reindex). Optional — when nil, classify + reindex
	// fall back to their pre-jobs synchronous behavior.
	Jobs jobs.Repo

	// AIProviders powers /v1/ai/providers (BYOM). Requires Vault to be
	// wired so api keys land sealed. Optional — when nil, the routes
	// aren't registered.
	AIProviders aiprovider.Repo
}

// NewMux returns an *http.ServeMux with every registered route. Callers
// may add more routes before wrapping the result in the auth/tenant/audit
// chain.
func NewMux(d Deps) *http.ServeMux {
	mux := http.NewServeMux()
	enq := d.Enqueuer
	if enq == nil {
		enq = worker.NoopEnqueuer{}
	}
	if d.Catalog != nil {
		registerCatalog(mux, d.Catalog)
	}
	if d.Pipelines != nil {
		pipelineHandlers(mux, d.Pipelines, enq, d.Deleted, d.LegalHolds)
	}
	if d.Quality != nil {
		checkHandlers(mux, d.Quality, enq, d.Deleted, d.LegalHolds)
	}
	if d.Notify != nil {
		notifyHandlers(mux, d.Notify)
	}
	if d.Policies != nil {
		policyHandlers(mux, d.Policies, d.Deleted, d.LegalHolds)
	}
	if d.Audit != nil {
		auditHandlers(mux, d.Audit)
	}
	if d.Deleted != nil {
		deletedHandlers(mux, d.Deleted, buildRestorers(d))
	}
	if d.LegalHolds != nil {
		legalHoldHandlers(mux, d.LegalHolds)
	}
	if d.DSR != nil {
		dsrHandlers(mux, d.DSR)
	}
	if d.Identity != nil {
		authDeps := AuthDeps{
			Identity: d.Identity,
			Email:    d.Email,
			Config:   d.AuthCfg,
		}
		authHandlers(mux, authDeps)
		teamHandlers(mux, authDeps)
		passwordResetHandlers(mux, authDeps)
		accountHandlers(mux, authDeps)
		accountGDPRHandlers(mux, authDeps)
	}
	if d.Connections != nil && d.ConnRegistry != nil {
		connectionHandlers(mux, ConnectionDeps{
			Connections: d.Connections,
			Vault:       d.Vault,
			Registry:    d.ConnRegistry,
			Enqueuer:    enq,
		})
	}
	if d.Pipelines != nil && d.Logs != nil {
		runLogsHandlers(mux, d.Pipelines, d.Logs)
	}
	if d.ColumnLineage != nil {
		columnLineageHandlers(mux, d.ColumnLineage)
	}
	if d.Glossary != nil {
		glossaryHandlers(mux, d.Glossary)
	}
	if d.Classifier != nil || d.Classifications != nil {
		classifyHandlers(mux, d.Classifier, d.Classifications, d.Jobs, enq)
	}
	if d.Jobs != nil {
		jobsHandlers(mux, d.Jobs)
	}
	if d.AIProviders != nil {
		aiProviderHandlers(mux, d.AIProviders, d.Vault)
	}
	if d.Catalog != nil && d.Policies != nil {
		accessHandlers(mux, d.Catalog, d.Policies, d.Identity)
	}
	if d.Catalog != nil {
		mountMCP(mux, d)
	}
	if d.SearchIndexer != nil && d.SearchSearcher != nil {
		semanticHandlers(mux, d.SearchIndexer, d.SearchSearcher, d.Policies, d.Jobs, enq)
	}
	mux.HandleFunc("GET /v1/stats", statsHandler(StatsDeps{
		Catalog:     d.Catalog,
		Pipelines:   d.Pipelines,
		Quality:     d.Quality,
		Deleted:     d.Deleted,
		LegalHolds:  d.LegalHolds,
		DSR:         d.DSR,
		Connections: d.Connections,
	}))
	return mux
}

// buildRestorers wires per-type restore functions for the recycle-bin
// endpoint. Each restorer re-INSERTs the tombstoned payload onto its
// source table. Domains without a Repo registered get no restorer (the
// recycle-bin handler returns 400 unsupported).
func buildRestorers(d Deps) map[string]Restorer {
	r := map[string]Restorer{}
	if d.Pipelines != nil {
		r["pipeline"] = pipelineRestorer(d.Pipelines)
	}
	if d.Quality != nil {
		r["check"] = checkRestorer(d.Quality)
	}
	if d.Policies != nil {
		r["policy"] = policyRestorer(d.Policies)
	}
	return r
}

// Mux is the legacy constructor: catalog-only. Prefer NewMux for new
// callers that need the orchestration / quality / notify / policy routes.
func Mux(store storage.Store) *http.ServeMux {
	return NewMux(Deps{Catalog: store})
}

func registerCatalog(mux *http.ServeMux, store storage.Store) {
	mux.HandleFunc("GET /v1/assets",                     listAssetsHandler(store))
	mux.HandleFunc("POST /v1/assets",                    createAssetHandler(store))
	mux.HandleFunc("GET /v1/assets/{id}",                getAssetHandler(store))
	mux.HandleFunc("PATCH /v1/assets/{id}",              updateAssetHandler(store))
	mux.HandleFunc("DELETE /v1/assets/{id}",             deleteAssetHandler(store))
	mux.HandleFunc("GET /v1/assets:byQualifiedName",     getByQNHandler(store))
	mux.HandleFunc("POST /v1/assets:search",             searchAssetsHandler(store))
	mux.HandleFunc("GET /v1/assets/{id}/lineage",        lineageHandler(store))
}

// ----- response helpers -----

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, graph.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody{"not_found", err.Error()})
	case errors.Is(err, graph.ErrConflict):
		writeJSON(w, http.StatusConflict, errorBody{"conflict", err.Error()})
	case errors.Is(err, graph.ErrInvalidArgument):
		writeJSON(w, http.StatusBadRequest, errorBody{"invalid_argument", err.Error()})
	case errors.Is(err, graph.ErrForbidden):
		writeJSON(w, http.StatusForbidden, errorBody{"forbidden", err.Error()})
	case errors.Is(err, graph.ErrTenantMissing):
		writeJSON(w, http.StatusUnauthorized, errorBody{"tenant_required", err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, errorBody{"internal", "internal error"})
	}
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	return nil
}
