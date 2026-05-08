package http

import (
	"net/http"
	"strconv"

	"github.com/Satyaamm/plowered/internal/core/graph"
	"github.com/Satyaamm/plowered/internal/storage"
)

// LineageResponse describes the subgraph rooted at a single asset.
type LineageResponse struct {
	Root      *graph.Asset      `json:"root"`
	Direction string            `json:"direction"`
	Depth     int               `json:"depth"`
	Edges     []LineageEdgeView `json:"edges"`
	Truncated bool              `json:"truncated,omitempty"`
}

type LineageEdgeView struct {
	ID       string `json:"id"`
	Source   string `json:"source_id"`
	Target   string `json:"target_id"`
	Kind     string `json:"kind"`
}

func lineageHandler(store storage.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		direction := r.URL.Query().Get("direction")
		if direction == "" {
			direction = "upstream"
		}
		depth := 1
		if s := r.URL.Query().Get("depth"); s != "" {
			if n, err := strconv.Atoi(s); err == nil {
				depth = n
			}
		}
		if depth <= 0 {
			depth = 1
		}
		if depth > 5 {
			depth = 5
		}

		root, err := store.GetAsset(r.Context(), id)
		if err != nil {
			writeError(w, err)
			return
		}

		outgoing := direction == "downstream"
		edges, err := store.Neighbors(r.Context(), root.ID, storage.NeighborsOptions{
			Kind:     graph.EdgeLineage,
			Outgoing: outgoing,
			Limit:    500,
		})
		if err != nil {
			writeError(w, err)
			return
		}

		out := make([]LineageEdgeView, 0, len(edges))
		for _, e := range edges {
			out = append(out, LineageEdgeView{
				ID:     e.ID,
				Source: e.SourceID,
				Target: e.TargetID,
				Kind:   string(e.Kind),
			})
		}

		writeJSON(w, http.StatusOK, LineageResponse{
			Root:      root,
			Direction: direction,
			Depth:     depth,
			Edges:     out,
			Truncated: len(out) >= 500,
		})
	}
}
