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
	"github.com/Satyaamm/plowered/internal/core/certification"
	"github.com/Satyaamm/plowered/internal/core/contract"
	"github.com/Satyaamm/plowered/internal/core/cost"
	"github.com/Satyaamm/plowered/internal/core/connection"
	"github.com/Satyaamm/plowered/internal/core/deleted"
	"github.com/Satyaamm/plowered/internal/core/dsr"
	"github.com/Satyaamm/plowered/internal/core/email"
	"github.com/Satyaamm/plowered/internal/core/feedback"
	"github.com/Satyaamm/plowered/internal/core/vectorstore"
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
	// Profiler runs per-column profile jobs (#4 feature). Optional;
	// nil disables /v1/assets/{id}/profile.
	Profiler           Profiler
	// Describer generates auto-description suggestions via the
	// tenant's configured chat provider (#7 feature). Optional.
	Describer          Describer
	// Asker is the Text-to-SQL surface (#6 feature). Optional.
	Asker              Asker
	// Migrator runs SQL→SQL data migration plans (#3 feature). Optional.
	Migrator           Migrator

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

	// Certification powers /v1/assets/{id}/certifications +
	// /v1/certifications/* (review queue, approve, reject, revoke).
	// Optional — when nil the routes aren't registered.
	Certification *certification.Service

	// Cost is the read surface for /v1/cost/*. Writers (AI completions,
	// warehouse queries) carry their own cost.Recorder reference, so
	// only the read side wires through here.
	Cost cost.Reader
	// CostBudgets exposes /v1/cost/budget. Optional — when nil, budget
	// endpoints aren't registered (read-only mode).
	CostBudgets cost.BudgetStore

	// Contract powers /v1/contracts/* and the per-asset
	// /v1/assets/{id}/contract surface. Optional — when nil the
	// routes aren't registered.
	Contract *contract.Service

	// Authorizer is the per-request RBAC + ABAC checkpoint. When nil,
	// NewMux derives one from d.Policies (or falls back to a permissive
	// AllowAll for memory mode + tests). Callers can pass their own to
	// override — e.g. an integration test that wants AllowAll regardless
	// of the wired policy store.
	Authorizer policy.Authorizer

	// Feedback exposes the user-feedback queue. Optional — when nil the
	// /v1/feedback routes aren't registered.
	Feedback feedback.Repo

	// VectorStores powers /v1/vectorstores. Optional — when nil the
	// routes aren't registered. The asset_embeddings + memory fallback
	// continues to serve search until a tenant configures one.
	VectorStores vectorstore.Repo

	// CloudStatus powers GET /v1/cloud/status (admin-gated, non-secret
	// infrastructure bindings). Optional — nil skips the route.
	CloudStatus *CloudStatus
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
	authz := d.Authorizer
	if authz == nil {
		// Default: wire the engine against the registered policy store so
		// per-resource ABAC rules apply. With no store, the engine still
		// runs role grants — only the deny-rule overrides go away.
		authz = policy.NewEngine(d.Policies)
	}
	if d.Catalog != nil {
		registerCatalog(mux, d.Catalog, authz)
	}
	if d.Pipelines != nil {
		pipelineHandlers(mux, d.Pipelines, enq, d.Deleted, d.LegalHolds, authz)
	}
	if d.Quality != nil {
		checkHandlers(mux, d.Quality, enq, d.Deleted, d.LegalHolds, authz)
	}
	if d.Notify != nil {
		notifyHandlers(mux, d.Notify, authz)
	}
	if d.Policies != nil {
		policyHandlers(mux, d.Policies, d.Deleted, d.LegalHolds, authz)
	}
	if d.Audit != nil {
		auditHandlers(mux, d.Audit, authz)
	}
	if d.Deleted != nil {
		deletedHandlers(mux, d.Deleted, buildRestorers(d), authz)
	}
	if d.LegalHolds != nil {
		legalHoldHandlers(mux, d.LegalHolds, authz)
	}
	if d.DSR != nil {
		dsrHandlers(mux, d.DSR, authz)
	}
	if d.Identity != nil {
		authDeps := AuthDeps{
			Identity:   d.Identity,
			Email:      d.Email,
			Config:     d.AuthCfg,
			Authorizer: authz,
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
			Authorizer:  authz,
		})
	}
	if d.Pipelines != nil && d.Logs != nil {
		runLogsHandlers(mux, d.Pipelines, d.Logs, authz)
	}
	if d.ColumnLineage != nil {
		columnLineageHandlers(mux, d.ColumnLineage, authz)
	}
	if d.Glossary != nil {
		glossaryHandlers(mux, d.Glossary, authz)
	}
	if d.Classifier != nil || d.Classifications != nil {
		classifyHandlers(mux, d.Classifier, d.Classifications, d.Jobs, enq, authz)
	}
	if d.Profiler != nil {
		profileHandlers(mux, d.Profiler, authz)
	}
	if d.Describer != nil {
		describeHandlers(mux, d.Describer, authz)
	}
	if d.Asker != nil {
		askHandlers(mux, d.Asker, authz)
	}
	if d.Migrator != nil {
		migrationHandlers(mux, d.Migrator, d.Enqueuer, authz)
	}
	if d.Jobs != nil {
		jobsHandlers(mux, d.Jobs, authz)
	}
	if d.AIProviders != nil {
		aiProviderHandlers(mux, d.AIProviders, d.Vault, authz)
	}
	if d.Certification != nil {
		certificationHandlers(mux, d.Certification, authz)
	}
	if d.Cost != nil {
		costHandlers(mux, d.Cost, d.CostBudgets, authz)
	}
	if d.Contract != nil {
		contractHandlers(mux, d.Contract, authz)
	}
	if d.Feedback != nil {
		feedbackHandlers(mux, d.Feedback, authz)
	}
	if d.VectorStores != nil {
		vectorStoreHandlers(mux, d.VectorStores, d.Vault, authz)
	}
	if d.CloudStatus != nil {
		cloudHandlers(mux, d.CloudStatus, authz)
	}
	if d.Catalog != nil && d.Policies != nil {
		accessHandlers(mux, d.Catalog, d.Policies, d.Identity, authz)
	}
	if d.Catalog != nil {
		mountMCP(mux, d)
	}
	if d.SearchIndexer != nil && d.SearchSearcher != nil {
		semanticHandlers(mux, d.SearchIndexer, d.SearchSearcher, d.Policies, d.Jobs, enq, authz)
	}
	mux.HandleFunc("GET /v1/stats", statsHandler(StatsDeps{
		Catalog:     d.Catalog,
		Pipelines:   d.Pipelines,
		Quality:     d.Quality,
		Deleted:     d.Deleted,
		LegalHolds:  d.LegalHolds,
		DSR:         d.DSR,
		Connections: d.Connections,
		Authorizer:  authz,
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

func registerCatalog(mux *http.ServeMux, store storage.Store, authz policy.Authorizer) {
	mux.HandleFunc("GET /v1/assets",                     listAssetsHandler(store, authz))
	mux.HandleFunc("POST /v1/assets",                    createAssetHandler(store, authz))
	mux.HandleFunc("GET /v1/assets/{id}",                getAssetHandler(store, authz))
	mux.HandleFunc("PATCH /v1/assets/{id}",              updateAssetHandler(store, authz))
	mux.HandleFunc("PATCH /v1/assets/{id}/owners",       updateAssetOwnersHandler(store, authz))
	mux.HandleFunc("DELETE /v1/assets/{id}",             deleteAssetHandler(store, authz))
	mux.HandleFunc("GET /v1/assets:byQualifiedName",     getByQNHandler(store, authz))
	mux.HandleFunc("POST /v1/assets:search",             searchAssetsHandler(store, authz))
	mux.HandleFunc("GET /v1/assets/{id}/lineage",        lineageHandler(store, authz))
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
