package http

import (
	"net/http"
	"strconv"

	"github.com/Satyaamm/plowered/internal/core/graph"
	"github.com/Satyaamm/plowered/internal/storage"
)

// LineageResponse describes the subgraph rooted at a single asset. Neighbor
// assets are inlined alongside edges so clients can render a graph in a
// single round-trip.
type LineageResponse struct {
	Root      *graph.Asset      `json:"root"`
	Direction string            `json:"direction"`
	Depth     int               `json:"depth"`
	Edges     []LineageEdgeView `json:"edges"`
	Neighbors []*graph.Asset    `json:"neighbors"`
	Truncated bool              `json:"truncated,omitempty"`
}

type LineageEdgeView struct {
	ID     string `json:"id"`
	Source string `json:"source_id"`
	Target string `json:"target_id"`
	Kind   string `json:"kind"`
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

		// Walk both directions explicitly when "both" is requested. Per-direction
		// queries are bounded by storage.NeighborsOptions.Limit so a pathological
		// fan-out can't OOM us.
		var edges []*graph.Edge
		switch direction {
		case "downstream":
			edges, err = store.Neighbors(r.Context(), root.ID, storage.NeighborsOptions{
				Kind: graph.EdgeLineage, Outgoing: true, Limit: 500,
			})
		case "both":
			up, errU := store.Neighbors(r.Context(), root.ID, storage.NeighborsOptions{
				Kind: graph.EdgeLineage, Outgoing: false, Limit: 500,
			})
			if errU != nil {
				writeError(w, errU)
				return
			}
			down, errD := store.Neighbors(r.Context(), root.ID, storage.NeighborsOptions{
				Kind: graph.EdgeLineage, Outgoing: true, Limit: 500,
			})
			if errD != nil {
				writeError(w, errD)
				return
			}
			edges = append(up, down...)
		default: // "upstream"
			edges, err = store.Neighbors(r.Context(), root.ID, storage.NeighborsOptions{
				Kind: graph.EdgeLineage, Outgoing: false, Limit: 500,
			})
		}
		if err != nil {
			writeError(w, err)
			return
		}

		// Hydrate neighbors. Deduplicate by id; the root is excluded from the
		// neighbors slice so the client renders it separately.
		seen := make(map[string]bool)
		neighbors := make([]*graph.Asset, 0, len(edges))
		for _, e := range edges {
			for _, peerID := range []string{e.SourceID, e.TargetID} {
				if peerID == root.ID || seen[peerID] {
					continue
				}
				seen[peerID] = true
				asset, err := store.GetAsset(r.Context(), peerID)
				if err != nil {
					continue // peer in a different RBAC scope; silently drop
				}
				neighbors = append(neighbors, asset)
			}
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
			Neighbors: neighbors,
			Truncated: len(edges) >= 500,
		})
	}
}
