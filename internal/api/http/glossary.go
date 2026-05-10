package http

import (
	"errors"
	"net/http"

	"github.com/Satyaamm/plowered/internal/core/glossary"
)

// glossaryHandlers registers term CRUD + assignment endpoints. Routes:
//
//	GET    /v1/glossary/terms
//	POST   /v1/glossary/terms
//	GET    /v1/glossary/terms/{id}
//	PATCH  /v1/glossary/terms/{id}
//	DELETE /v1/glossary/terms/{id}
//	POST   /v1/glossary/terms/{id}/assignments      body: {asset_id}
//	DELETE /v1/glossary/terms/{id}/assignments/{assetId}
//	GET    /v1/glossary/terms/{id}/assets           — assets linked
//	GET    /v1/assets/{id}/terms                    — terms linked
func glossaryHandlers(mux *http.ServeMux, repo glossary.Repo) {
	mux.HandleFunc("GET /v1/glossary/terms", listTermsHandler(repo))
	mux.HandleFunc("POST /v1/glossary/terms", createTermHandler(repo))
	mux.HandleFunc("GET /v1/glossary/terms/{id}", getTermHandler(repo))
	mux.HandleFunc("PATCH /v1/glossary/terms/{id}", updateTermHandler(repo))
	mux.HandleFunc("DELETE /v1/glossary/terms/{id}", deleteTermHandler(repo))
	mux.HandleFunc("POST /v1/glossary/terms/{id}/assignments", assignTermHandler(repo))
	mux.HandleFunc("DELETE /v1/glossary/terms/{id}/assignments/{assetId}", unassignTermHandler(repo))
	mux.HandleFunc("GET /v1/glossary/terms/{id}/assets", assetsForTermHandler(repo))
	mux.HandleFunc("GET /v1/assets/{id}/terms", termsForAssetHandler(repo))
}

type termPayload struct {
	Name       string `json:"name"`
	Definition string `json:"definition"`
	ParentID   string `json:"parent_id"`
	Status     string `json:"status"`
	OwnerID    string `json:"owner_id"`
}

type termView struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Definition string `json:"definition"`
	ParentID   string `json:"parent_id,omitempty"`
	Status     string `json:"status"`
	OwnerID    string `json:"owner_id,omitempty"`
	CreatedAt  string `json:"created_at"`
	UpdatedAt  string `json:"updated_at"`
}

func toTermView(t *glossary.Term) termView {
	return termView{
		ID: t.ID, Name: t.Name, Definition: t.Definition,
		ParentID: t.ParentID, Status: string(t.Status), OwnerID: t.OwnerID,
		CreatedAt: t.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt: t.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}

func listTermsHandler(r glossary.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		tenant := mustTenant(w, req)
		if tenant == "" {
			return
		}
		terms, err := r.List(req.Context(), tenant)
		if err != nil {
			writeError(w, err)
			return
		}
		out := make([]termView, 0, len(terms))
		for _, t := range terms {
			out = append(out, toTermView(t))
		}
		writeJSON(w, http.StatusOK, map[string]any{"terms": out})
	}
}

func createTermHandler(r glossary.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		tenant := mustTenant(w, req)
		if tenant == "" {
			return
		}
		var p termPayload
		if err := decodeJSON(req, &p); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if p.Name == "" {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "name required"})
			return
		}
		t := &glossary.Term{
			TenantID: tenant, Name: p.Name, Definition: p.Definition,
			ParentID: p.ParentID, OwnerID: p.OwnerID,
			Status: glossary.Status(p.Status),
		}
		if pr, ok := principalFrom(req); ok && t.OwnerID == "" {
			t.OwnerID = pr.ID
		}
		got, err := r.Create(req.Context(), t)
		if err != nil {
			if errors.Is(err, glossary.ErrNameTaken) {
				writeJSON(w, http.StatusConflict, errorBody{"name_taken", err.Error()})
				return
			}
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, toTermView(got))
	}
}

func getTermHandler(r glossary.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		tenant := mustTenant(w, req)
		if tenant == "" {
			return
		}
		got, err := r.Get(req.Context(), tenant, req.PathValue("id"))
		if err != nil {
			if errors.Is(err, glossary.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, errorBody{"not_found", "term not found"})
				return
			}
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toTermView(got))
	}
}

func updateTermHandler(r glossary.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		tenant := mustTenant(w, req)
		if tenant == "" {
			return
		}
		id := req.PathValue("id")
		existing, err := r.Get(req.Context(), tenant, id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, errorBody{"not_found", "term not found"})
			return
		}
		var p termPayload
		if err := decodeJSON(req, &p); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if p.Name != "" {
			existing.Name = p.Name
		}
		existing.Definition = p.Definition
		existing.ParentID = p.ParentID
		if p.Status != "" {
			existing.Status = glossary.Status(p.Status)
		}
		if p.OwnerID != "" {
			existing.OwnerID = p.OwnerID
		}
		got, err := r.Update(req.Context(), existing)
		if err != nil {
			if errors.Is(err, glossary.ErrNameTaken) {
				writeJSON(w, http.StatusConflict, errorBody{"name_taken", err.Error()})
				return
			}
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, toTermView(got))
	}
}

func deleteTermHandler(r glossary.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		tenant := mustTenant(w, req)
		if tenant == "" {
			return
		}
		if err := r.Delete(req.Context(), tenant, req.PathValue("id")); err != nil {
			if errors.Is(err, glossary.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, errorBody{"not_found", "term not found"})
				return
			}
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

type assignBody struct {
	AssetID string `json:"asset_id"`
}

func assignTermHandler(r glossary.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		tenant := mustTenant(w, req)
		if tenant == "" {
			return
		}
		var p assignBody
		if err := decodeJSON(req, &p); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if p.AssetID == "" {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "asset_id required"})
			return
		}
		actor := ""
		if pr, ok := principalFrom(req); ok {
			actor = pr.ID
		}
		err := r.Assign(req.Context(), &glossary.Assignment{
			TenantID: tenant, TermID: req.PathValue("id"),
			AssetID: p.AssetID, AssignedBy: actor,
		})
		if err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func unassignTermHandler(r glossary.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		tenant := mustTenant(w, req)
		if tenant == "" {
			return
		}
		err := r.Unassign(req.Context(), tenant, req.PathValue("id"), req.PathValue("assetId"))
		if err != nil {
			if errors.Is(err, glossary.ErrNotFound) {
				writeJSON(w, http.StatusNotFound, errorBody{"not_found", "assignment not found"})
				return
			}
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func assetsForTermHandler(r glossary.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		tenant := mustTenant(w, req)
		if tenant == "" {
			return
		}
		ids, err := r.AssetsByTerm(req.Context(), tenant, req.PathValue("id"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"asset_ids": ids})
	}
}

func termsForAssetHandler(r glossary.Repo) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		tenant := mustTenant(w, req)
		if tenant == "" {
			return
		}
		assignments, err := r.AssignmentsByAsset(req.Context(), tenant, req.PathValue("id"))
		if err != nil {
			writeError(w, err)
			return
		}
		out := make([]map[string]any, 0, len(assignments))
		for _, a := range assignments {
			out = append(out, map[string]any{
				"term_id":     a.TermID,
				"name":        a.TermName,
				"definition":  a.Definition,
				"status":      string(a.Status),
				"asset_id":    a.AssetID,
				"assigned_at": a.AssignedAt.Format("2006-01-02T15:04:05Z07:00"),
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"terms": out})
	}
}
