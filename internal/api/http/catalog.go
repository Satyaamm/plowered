package http

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/Satyaamm/plowered/internal/core/graph"
	"github.com/Satyaamm/plowered/internal/storage"
)

func createAssetHandler(store storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var a graph.Asset
		if err := decodeJSON(r, &a); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if err := graph.ValidateAsset(&a); err != nil {
			writeError(w, err)
			return
		}
		got, err := store.CreateAsset(r.Context(), &a)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, got)
	}
}

func getAssetHandler(store storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got, err := store.GetAsset(r.Context(), r.PathValue("id"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, got)
	}
}

func getByQNHandler(store storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		qn := r.URL.Query().Get("qn")
		if qn == "" {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "qn required"})
			return
		}
		got, err := store.GetAssetByQualifiedName(r.Context(), qn)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, got)
	}
}

func updateAssetHandler(store storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var a graph.Asset
		if err := decodeJSON(r, &a); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		a.ID = r.PathValue("id")
		if err := graph.ValidateAsset(&a); err != nil {
			writeError(w, err)
			return
		}
		got, err := store.UpdateAsset(r.Context(), &a)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, got)
	}
}

// updateAssetOwnersHandler is a focused partial-update endpoint for
// the owners field. The general updateAssetHandler does a full replace
// (every field of the asset row gets overwritten) which means a
// "change owner" click would have to fetch-merge-PUT to avoid blanking
// description / tags / trust / etc. This endpoint does the merge
// server-side: fetch existing, swap Owners, save.
//
// Atomic at the per-asset row level (one transaction, one update).
// Doesn't cover concurrent owner edits from two tabs — last write
// wins, same as the rest of the catalog mutations.
func updateAssetOwnersHandler(store storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Owners []string `json:"owners"`
		}
		if err := decodeJSON(r, &body); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		existing, err := store.GetAsset(r.Context(), r.PathValue("id"))
		if err != nil {
			writeError(w, err)
			return
		}
		existing.Owners = body.Owners
		if err := graph.ValidateAsset(existing); err != nil {
			writeError(w, err)
			return
		}
		got, err := store.UpdateAsset(r.Context(), existing)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, got)
	}
}

func deleteAssetHandler(store storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := store.DeleteAsset(r.Context(), r.PathValue("id")); err != nil {
			writeError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func listAssetsHandler(store storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		opts := storage.ListAssetsOptions{
			Type:      graph.AssetType(q.Get("type")),
			ParentID:  q.Get("parent_id"),
			PageToken: q.Get("page_token"),
		}
		if s := q.Get("page_size"); s != "" {
			if n, err := strconv.Atoi(s); err == nil {
				opts.PageSize = n
			}
		}
		assets, next, err := store.ListAssets(r.Context(), opts)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"assets":          assets,
			"next_page_token": next,
		})
	}
}

// SearchRequest is the JSON body for /v1/assets:search.
type SearchRequest struct {
	Query  string `json:"query"`
	Limit  int    `json:"limit"`
	Type   string `json:"type,omitempty"`
}

type SearchHit struct {
	Asset *graph.Asset `json:"asset"`
	Score float64      `json:"score"`
}

func searchAssetsHandler(store storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req SearchRequest
		if err := decodeJSON(r, &req); err != nil {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", err.Error()})
			return
		}
		if req.Query == "" {
			writeJSON(w, http.StatusBadRequest, errorBody{"bad_request", "query required"})
			return
		}
		if req.Limit <= 0 || req.Limit > 100 {
			req.Limit = 20
		}

		// v0: list and filter by qualified-name / name substring. The search
		// package replaces this when it lands.
		opts := storage.ListAssetsOptions{
			Type:     graph.AssetType(req.Type),
			PageSize: 500,
		}
		assets, _, err := store.ListAssets(r.Context(), opts)
		if err != nil {
			writeError(w, err)
			return
		}

		needle := strings.ToLower(req.Query)
		hits := make([]SearchHit, 0, req.Limit)
		for _, a := range assets {
			score := matchScore(a, needle)
			if score == 0 {
				continue
			}
			hits = append(hits, SearchHit{Asset: a, Score: score})
			if len(hits) >= req.Limit {
				break
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"hits": hits})
	}
}

// matchScore is a cheap lexical scorer used until the search package replaces
// list-and-filter. Exact name match scores highest, then qualified-name
// containment, then description containment.
func matchScore(a *graph.Asset, needle string) float64 {
	switch {
	case strings.EqualFold(a.Name, needle):
		return 1.0
	case strings.Contains(strings.ToLower(a.QualifiedName), needle):
		return 0.7
	case strings.Contains(strings.ToLower(a.Name), needle):
		return 0.5
	case strings.Contains(strings.ToLower(a.Description), needle):
		return 0.3
	}
	return 0
}
