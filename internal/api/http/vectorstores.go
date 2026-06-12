package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/Satyaamm/plowered/internal/core/policy"
	"github.com/Satyaamm/plowered/internal/core/secrets"
	"github.com/Satyaamm/plowered/internal/core/vectorstore"
)

// vectorStoreHandlers exposes the per-tenant vector-store config
// surface. Mirrors /v1/ai/providers — a tenant admin picks a backend
// (pgvector / Pinecone / Weaviate / Qdrant / memory), supplies the
// per-kind fields + an api key, and the platform stores the row +
// seals the key in the vault.
//
//	GET    /v1/vectorstores              list
//	POST   /v1/vectorstores              create + store key in vault
//	GET    /v1/vectorstores/{id}         read
//	PATCH  /v1/vectorstores/{id}         update + rotate key
//	DELETE /v1/vectorstores/{id}         delete + scrub key
//	POST   /v1/vectorstores/{id}/test    re-test stored creds
//	POST   /v1/vectorstores/{id}/primary mark primary
//	POST   /v1/vectorstores:test         pre-save credential probe
//
// Admin-gated end to end: write paths require VerbAdmin (credentials
// in the vault); reads require VerbRead because seeing the wired
// backend is useful for non-admin developers.
func vectorStoreHandlers(mux *http.ServeMux, repo vectorstore.Repo, vault secrets.Vault, authz policy.Authorizer) {
	if repo == nil {
		return
	}
	mux.HandleFunc("GET    /v1/vectorstores",                listVectorStoreHandler(repo, authz))
	mux.HandleFunc("POST   /v1/vectorstores",                createVectorStoreHandler(repo, vault, authz))
	mux.HandleFunc("GET    /v1/vectorstores/{id}",           getVectorStoreHandler(repo, authz))
	mux.HandleFunc("PATCH  /v1/vectorstores/{id}",           updateVectorStoreHandler(repo, vault, authz))
	mux.HandleFunc("DELETE /v1/vectorstores/{id}",           deleteVectorStoreHandler(repo, vault, authz))
	mux.HandleFunc("POST   /v1/vectorstores/{id}/test",      testStoredVectorStoreHandler(repo, vault, authz))
	mux.HandleFunc("POST   /v1/vectorstores/{id}/primary",   primaryVectorStoreHandler(repo, authz))
	mux.HandleFunc("POST   /v1/vectorstores:test",           testInlineVectorStoreHandler(authz))
}

type vectorStoreReq struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Endpoint   string `json:"endpoint,omitempty"`
	IndexName  string `json:"index_name,omitempty"`
	ClassName  string `json:"class_name,omitempty"`
	Collection string `json:"collection,omitempty"`
	Dimension  int    `json:"dimension,omitempty"`
	APIKey     string `json:"api_key,omitempty"`
	IsPrimary  bool   `json:"is_primary,omitempty"`
}

func (r vectorStoreReq) validate(requireKey bool) error {
	if r.Name == "" {
		return errors.New("name is required")
	}
	kind := vectorstore.Kind(r.Kind)
	if !isKnownVectorStoreKind(kind) {
		return errors.New("kind must be one of: pgvector | memory | pinecone | weaviate | qdrant")
	}
	switch kind {
	case vectorstore.KindPinecone:
		if r.Endpoint == "" || r.IndexName == "" {
			return errors.New("pinecone requires endpoint + index_name")
		}
		if requireKey && r.APIKey == "" {
			return errors.New("pinecone requires api_key")
		}
	case vectorstore.KindWeaviate:
		if r.Endpoint == "" || r.ClassName == "" {
			return errors.New("weaviate requires endpoint + class_name")
		}
	case vectorstore.KindQdrant:
		if r.Endpoint == "" || r.Collection == "" {
			return errors.New("qdrant requires endpoint + collection")
		}
	}
	return nil
}

func isKnownVectorStoreKind(k vectorstore.Kind) bool {
	for _, kk := range vectorstore.AllKinds {
		if kk == k {
			return true
		}
	}
	return false
}

func listVectorStoreHandler(repo vectorstore.Repo, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := gateTenantAndVerb(w, r, authz, policy.VerbRead, "vector_store")
		if tenant == "" {
			return
		}
		out, err := repo.List(r.Context(), tenant)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"vectorstores": redactList(out)})
	}
}

func createVectorStoreHandler(repo vectorstore.Repo, vault secrets.Vault, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := gateTenantAndVerb(w, r, authz, policy.VerbAdmin, "vector_store")
		if tenant == "" {
			return
		}
		var req vectorStoreReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if err := req.validate(true); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		cfg := &vectorstore.Config{
			TenantID:   tenant,
			Kind:       vectorstore.Kind(req.Kind),
			Name:       req.Name,
			Endpoint:   req.Endpoint,
			IndexName:  req.IndexName,
			ClassName:  req.ClassName,
			Collection: req.Collection,
			Dimension:  req.Dimension,
			IsPrimary:  req.IsPrimary,
		}
		created, err := repo.Create(r.Context(), cfg)
		if err != nil {
			writeError(w, err)
			return
		}
		// Pinecone / Weaviate / Qdrant: stash the api key under a
		// deterministic URN derived from the row id, then patch the
		// row with that URN.
		if req.APIKey != "" {
			if vault == nil {
				_ = repo.Delete(r.Context(), tenant, created.ID)
				writeJSON(w, http.StatusServiceUnavailable, errorBody{"vault_unavailable", "secrets vault not configured"})
				return
			}
			urn := vectorstore.SecretURNFor(created.ID)
			if err := vault.Put(r.Context(), tenant, urn, []byte(req.APIKey)); err != nil {
				_ = repo.Delete(r.Context(), tenant, created.ID)
				writeJSON(w, http.StatusInternalServerError, errorBody{"vault_write_failed", err.Error()})
				return
			}
			if err := repo.SetSecretURN(r.Context(), tenant, created.ID, urn); err != nil {
				writeJSON(w, http.StatusInternalServerError, errorBody{"persist_failed", err.Error()})
				return
			}
			created.SecretURN = urn
		}
		if req.IsPrimary {
			_ = repo.SetPrimary(r.Context(), tenant, created.ID)
		}
		writeJSON(w, http.StatusCreated, redact(created))
	}
}

func getVectorStoreHandler(repo vectorstore.Repo, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := gateTenantAndVerb(w, r, authz, policy.VerbRead, "vector_store")
		if tenant == "" {
			return
		}
		c, err := repo.Get(r.Context(), tenant, r.PathValue("id"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, redact(c))
	}
}

func updateVectorStoreHandler(repo vectorstore.Repo, vault secrets.Vault, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := gateTenantAndVerb(w, r, authz, policy.VerbAdmin, "vector_store")
		if tenant == "" {
			return
		}
		var req vectorStoreReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if err := req.validate(false); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		id := r.PathValue("id")
		existing, err := repo.Get(r.Context(), tenant, id)
		if err != nil {
			writeError(w, err)
			return
		}
		existing.Name = req.Name
		existing.Endpoint = req.Endpoint
		existing.IndexName = req.IndexName
		existing.ClassName = req.ClassName
		existing.Collection = req.Collection
		existing.Dimension = req.Dimension
		updated, err := repo.Update(r.Context(), existing)
		if err != nil {
			writeError(w, err)
			return
		}
		if req.APIKey != "" {
			if vault == nil {
				writeJSON(w, http.StatusServiceUnavailable, errorBody{"vault_unavailable", "secrets vault not configured"})
				return
			}
			if err := vault.Put(r.Context(), tenant, existing.SecretURN, []byte(req.APIKey)); err != nil {
				writeJSON(w, http.StatusInternalServerError, errorBody{"vault_write_failed", err.Error()})
				return
			}
		}
		if req.IsPrimary && !existing.IsPrimary {
			_ = repo.SetPrimary(r.Context(), tenant, existing.ID)
		}
		writeJSON(w, http.StatusOK, redact(updated))
	}
}

func deleteVectorStoreHandler(repo vectorstore.Repo, vault secrets.Vault, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := gateTenantAndVerb(w, r, authz, policy.VerbAdmin, "vector_store")
		if tenant == "" {
			return
		}
		id := r.PathValue("id")
		existing, err := repo.Get(r.Context(), tenant, id)
		if err != nil {
			writeError(w, err)
			return
		}
		if err := repo.Delete(r.Context(), tenant, id); err != nil {
			writeError(w, err)
			return
		}
		if vault != nil && existing.SecretURN != "" {
			_ = vault.Delete(r.Context(), tenant, existing.SecretURN)
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func testStoredVectorStoreHandler(repo vectorstore.Repo, vault secrets.Vault, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := gateTenantAndVerb(w, r, authz, policy.VerbAdmin, "vector_store")
		if tenant == "" {
			return
		}
		cfg, err := repo.Get(r.Context(), tenant, r.PathValue("id"))
		if err != nil {
			writeError(w, err)
			return
		}
		var secret []byte
		if cfg.SecretURN != "" && vault != nil {
			secret, _ = vault.Get(r.Context(), tenant, cfg.SecretURN)
		}
		testErr := vectorstore.Test(r.Context(), cfg, secret)
		ok := testErr == nil
		errMsg := ""
		if testErr != nil {
			errMsg = testErr.Error()
		}
		_ = repo.MarkTested(r.Context(), tenant, cfg.ID, ok, errMsg)
		status := http.StatusOK
		if !ok {
			status = http.StatusFailedDependency
		}
		writeJSON(w, status, map[string]any{"ok": ok, "error": errMsg})
	}
}

func primaryVectorStoreHandler(repo vectorstore.Repo, authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := gateTenantAndVerb(w, r, authz, policy.VerbAdmin, "vector_store")
		if tenant == "" {
			return
		}
		if err := repo.SetPrimary(r.Context(), tenant, r.PathValue("id")); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func testInlineVectorStoreHandler(authz policy.Authorizer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := gateTenantAndVerb(w, r, authz, policy.VerbAdmin, "vector_store")
		if tenant == "" {
			return
		}
		var req vectorStoreReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if err := req.validate(true); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		cfg := &vectorstore.Config{
			TenantID:   tenant,
			Kind:       vectorstore.Kind(req.Kind),
			Name:       req.Name,
			Endpoint:   req.Endpoint,
			IndexName:  req.IndexName,
			ClassName:  req.ClassName,
			Collection: req.Collection,
			Dimension:  req.Dimension,
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10_000_000_000) // 10s
		defer cancel()
		err := vectorstore.Test(ctx, cfg, []byte(req.APIKey))
		ok := err == nil
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": ok, "error": msg})
	}
}

// vectorStoreView is the wire shape. Never includes SecretURN.
type vectorStoreView struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Endpoint     string `json:"endpoint,omitempty"`
	IndexName    string `json:"index_name,omitempty"`
	ClassName    string `json:"class_name,omitempty"`
	Collection   string `json:"collection,omitempty"`
	Dimension    int    `json:"dimension,omitempty"`
	IsPrimary    bool   `json:"is_primary"`
	LastTestedAt string `json:"last_tested_at,omitempty"`
	LastTestOK   bool   `json:"last_test_ok"`
	LastTestErr  string `json:"last_test_error,omitempty"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

func redact(c *vectorstore.Config) vectorStoreView {
	v := vectorStoreView{
		ID: c.ID, Kind: string(c.Kind), Name: c.Name, Endpoint: c.Endpoint,
		IndexName: c.IndexName, ClassName: c.ClassName, Collection: c.Collection,
		Dimension: c.Dimension, IsPrimary: c.IsPrimary,
		LastTestOK: c.LastTestOK, LastTestErr: c.LastTestErr,
		CreatedAt: c.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: c.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
	if !c.LastTestedAt.IsZero() {
		v.LastTestedAt = c.LastTestedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return v
}

func redactList(in []*vectorstore.Config) []vectorStoreView {
	out := make([]vectorStoreView, 0, len(in))
	for _, c := range in {
		out = append(out, redact(c))
	}
	return out
}
