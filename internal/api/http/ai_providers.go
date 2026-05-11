package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/Satyaamm/plowered/internal/core/aiprovider"
	"github.com/Satyaamm/plowered/internal/core/secrets"
)

// aiProviderHandlers exposes the BYOM configuration surface:
//
//	GET    /v1/ai/providers              list configs for tenant
//	POST   /v1/ai/providers              create + store api key in vault
//	PATCH  /v1/ai/providers/{id}         update metadata (and rotate key)
//	DELETE /v1/ai/providers/{id}         delete config + vault entry
//	POST   /v1/ai/providers/{id}/test    re-test stored credentials
//	POST   /v1/ai/providers/{id}/primary mark as primary for its capability
//	POST   /v1/ai/providers:test         pre-save credential probe (no
//	                                      persistence; the settings UI
//	                                      enables Save only on success)
//
// The api_key field is write-only. List responses use the Redacted shape
// which never carries the URN or any cached key material.
func aiProviderHandlers(mux *http.ServeMux, repo aiprovider.Repo, vault secrets.Vault) {
	mux.HandleFunc("GET /v1/ai/providers", listAIProvidersHandler(repo))
	mux.HandleFunc("POST /v1/ai/providers", createAIProviderHandler(repo, vault))
	mux.HandleFunc("PATCH /v1/ai/providers/{id}", updateAIProviderHandler(repo, vault))
	mux.HandleFunc("DELETE /v1/ai/providers/{id}", deleteAIProviderHandler(repo, vault))
	mux.HandleFunc("POST /v1/ai/providers/{id}/test", testStoredAIProviderHandler(repo, vault))
	mux.HandleFunc("POST /v1/ai/providers/{id}/primary", primaryAIProviderHandler(repo))
	mux.HandleFunc("POST /v1/ai/providers:test", testInlineAIProviderHandler())
}

type aiProviderReq struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Model      string `json:"model"`
	BaseURL    string `json:"base_url,omitempty"`
	APIKey     string `json:"api_key,omitempty"`
	Capability string `json:"capability"`
	IsPrimary  bool   `json:"is_primary,omitempty"`
}

func (r aiProviderReq) validate(ctx context.Context, requireKey bool) error {
	if r.Name == "" {
		return errors.New("name is required")
	}
	if r.Model == "" {
		return errors.New("model is required")
	}
	if !isKnownKind(r.Kind) {
		return errors.New("kind must be one of: anthropic, openai, deepseek, openai-compatible")
	}
	if r.Kind == string(aiprovider.KindCustom) && r.BaseURL == "" {
		return errors.New("base_url is required for openai-compatible providers")
	}
	if !isKnownCapability(r.Capability) {
		return errors.New("capability must be either 'chat' or 'embed'")
	}
	if requireKey && r.APIKey == "" {
		return errors.New("api_key is required")
	}
	// SSRF guard: reject base_urls that resolve to private / loopback /
	// metadata IPs. Skipped for empty base_urls (the well-known
	// providers' defaults are public domains we trust).
	if strings.TrimSpace(r.BaseURL) != "" {
		if err := aiprovider.ValidateBaseURL(ctx, r.BaseURL); err != nil {
			return fmt.Errorf("base_url rejected: %w", err)
		}
	}
	return nil
}

func isKnownKind(k string) bool {
	for _, kk := range aiprovider.AllKinds {
		if string(kk) == k {
			return true
		}
	}
	return false
}

func isKnownCapability(c string) bool {
	return c == string(aiprovider.CapChat) || c == string(aiprovider.CapEmbed)
}

func listAIProvidersHandler(repo aiprovider.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		configs, err := repo.List(r.Context(), tenant)
		if err != nil {
			writeError(w, err)
			return
		}
		out := make([]aiprovider.Redacted, 0, len(configs))
		for _, c := range configs {
			out = append(out, c.Redact())
		}
		writeJSON(w, http.StatusOK, map[string]any{"providers": out})
	}
}

func createAIProviderHandler(repo aiprovider.Repo, vault secrets.Vault) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		if vault == nil {
			writeJSON(w, http.StatusServiceUnavailable, errorBody{"vault_unavailable", "secrets vault is not configured on this deployment"})
			return
		}
		var req aiProviderReq
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if err := req.validate(r.Context(), true); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		cfg := &aiprovider.Config{
			TenantID:   tenant,
			Kind:       aiprovider.Kind(req.Kind),
			Name:       req.Name,
			Model:      req.Model,
			BaseURL:    req.BaseURL,
			Capability: aiprovider.Capability(req.Capability),
			IsPrimary:  req.IsPrimary,
		}
		created, err := repo.Create(r.Context(), cfg)
		if err != nil {
			writeError(w, err)
			return
		}
		urn := aiprovider.SecretURNFor(created.ID)
		if err := vault.Put(r.Context(), tenant, urn, []byte(req.APIKey)); err != nil {
			// Best-effort cleanup so we don't leave an orphan config
			// row pointing at a key that doesn't exist in the vault.
			_ = repo.Delete(r.Context(), tenant, created.ID)
			writeJSON(w, http.StatusInternalServerError, errorBody{"vault_write_failed", err.Error()})
			return
		}
		created.SecretURN = urn
		if _, err := repo.Update(r.Context(), created); err != nil {
			writeError(w, err)
			return
		}
		// Persist the URN on the row. Update() doesn't write secret_urn
		// so we issue a focused write here.
		// (Done via raw helper to keep Update's surface narrow.)
		if err := setSecretURN(r.Context(), repo, tenant, created.ID, urn); err != nil {
			writeJSON(w, http.StatusInternalServerError, errorBody{"persist_failed", err.Error()})
			return
		}
		if req.IsPrimary {
			_ = repo.SetPrimary(r.Context(), tenant, created.ID)
		}
		writeJSON(w, http.StatusCreated, created.Redact())
	}
}

// setSecretURN is a tiny helper to avoid widening the Repo.Update
// signature just for the secret_urn write. The Postgres impl exposes a
// raw setter; the in-memory test impl can re-use Update's path.
func setSecretURN(ctx context.Context, repo aiprovider.Repo, tenantID, id, urn string) error {
	type urnSetter interface {
		SetSecretURN(ctx context.Context, tenantID, id, urn string) error
	}
	if s, ok := repo.(urnSetter); ok {
		return s.SetSecretURN(ctx, tenantID, id, urn)
	}
	return nil
}

func updateAIProviderHandler(repo aiprovider.Repo, vault secrets.Vault) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		id := r.PathValue("id")
		var req aiProviderReq
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		// Rotating the key is optional on PATCH.
		if err := req.validate(r.Context(), false); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		existing, err := repo.Get(r.Context(), tenant, id)
		if err != nil {
			writeError(w, err)
			return
		}
		existing.Name = req.Name
		existing.Model = req.Model
		existing.BaseURL = req.BaseURL
		existing.Capability = aiprovider.Capability(req.Capability)
		updated, err := repo.Update(r.Context(), existing)
		if err != nil {
			writeError(w, err)
			return
		}
		if req.APIKey != "" {
			if vault == nil {
				writeJSON(w, http.StatusServiceUnavailable, errorBody{"vault_unavailable", "secrets vault is not configured"})
				return
			}
			if err := vault.Put(r.Context(), tenant, existing.SecretURN, []byte(req.APIKey)); err != nil {
				writeJSON(w, http.StatusInternalServerError, errorBody{"vault_write_failed", err.Error()})
				return
			}
			_ = repo.MarkTested(r.Context(), tenant, existing.ID, false, "")
		}
		if req.IsPrimary && !existing.IsPrimary {
			_ = repo.SetPrimary(r.Context(), tenant, existing.ID)
		}
		writeJSON(w, http.StatusOK, updated.Redact())
	}
}

func deleteAIProviderHandler(repo aiprovider.Repo, vault secrets.Vault) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
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

func testStoredAIProviderHandler(repo aiprovider.Repo, vault secrets.Vault) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		id := r.PathValue("id")
		cfg, err := repo.Get(r.Context(), tenant, id)
		if err != nil {
			writeError(w, err)
			return
		}
		if vault == nil {
			writeJSON(w, http.StatusServiceUnavailable, errorBody{"vault_unavailable", "secrets vault is not configured"})
			return
		}
		key, err := vault.Get(r.Context(), tenant, cfg.SecretURN)
		if err != nil {
			writeJSON(w, http.StatusFailedDependency, errorBody{"vault_read_failed", err.Error()})
			return
		}
		testErr := aiprovider.Test(r.Context(), cfg, key)
		ok := testErr == nil
		var errMsg string
		if testErr != nil {
			errMsg = testErr.Error()
		}
		_ = repo.MarkTested(r.Context(), tenant, id, ok, errMsg)
		status := http.StatusOK
		if !ok {
			status = http.StatusFailedDependency
		}
		writeJSON(w, status, map[string]any{
			"ok":    ok,
			"error": errMsg,
		})
	}
}

func primaryAIProviderHandler(repo aiprovider.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
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

// testInlineAIProviderHandler powers the settings page's "Test" button
// before save: the user fills the form, clicks Test, we probe the
// supplied (kind, model, base_url, api_key) with no persistence. The
// Save button stays disabled until this returns ok=true.
func testInlineAIProviderHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		var req aiProviderReq
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if err := req.validate(r.Context(), true); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		cfg := &aiprovider.Config{
			TenantID: tenant,
			Kind:     aiprovider.Kind(req.Kind),
			Model:    req.Model,
			BaseURL:  req.BaseURL,
		}
		testErr := aiprovider.Test(r.Context(), cfg, []byte(req.APIKey))
		if testErr != nil {
			writeJSON(w, http.StatusFailedDependency, map[string]any{
				"ok":    false,
				"error": testErr.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}
