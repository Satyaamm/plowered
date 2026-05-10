package http

import (
	"context"
	"net/http"

	"github.com/Satyaamm/plowered/internal/core/deleted"
	"github.com/Satyaamm/plowered/internal/core/legalhold"
	"github.com/Satyaamm/plowered/internal/core/policy"
)

func policyHandlers(mux *http.ServeMux, store policy.RuleRepo, tomb deleted.Repo, holds legalhold.Repo) {
	mux.HandleFunc("GET /v1/policies",            listPoliciesHandler(store))
	mux.HandleFunc("POST /v1/policies",           createPolicyHandler(store))
	mux.HandleFunc("DELETE /v1/policies/{id}",    deletePolicyHandler(store, tomb, holds))
}

func listPoliciesHandler(s policy.RuleRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"rules": s.ListRules(tenant)})
	}
}

func createPolicyHandler(s policy.RuleRepo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		var rule policy.Rule
		if err := decodeJSON(r, &rule); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if rule.Effect == "" || len(rule.Verbs) == 0 {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "effect and verbs required"})
			return
		}
		rule.TenantID = tenant
		out := s.AddRule(rule)
		writeJSON(w, http.StatusCreated, out)
	}
}

func deletePolicyHandler(s policy.RuleRepo, tomb deleted.Repo, holds legalhold.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := mustTenant(w, r)
		if tenant == "" {
			return
		}
		id := r.PathValue("id")
		// Locate the rule before deleting so we can stash it as a tombstone.
		var matched *policy.Rule
		for _, ru := range s.ListRules(tenant) {
			if ru.ID == id {
				rcopy := ru
				matched = &rcopy
				break
			}
		}
		if matched == nil {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", "rule not found"})
			return
		}
		if tomb != nil {
			if err := captureTombstone(r, tomb, holds, "policy", matched.ID, matched); err != nil {
				writeDeleteError(w, err)
				return
			}
		}
		if !s.DeleteRule(tenant, id) {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", "rule not found"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func policyRestorer(s policy.RuleRepo) Restorer {
	return func(_ context.Context, rec *deleted.Record) error {
		var rule policy.Rule
		if err := remapJSON(rec.Payload, &rule); err != nil {
			return err
		}
		s.AddRule(rule)
		return nil
	}
}
