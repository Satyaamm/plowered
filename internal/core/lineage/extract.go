package lineage

import (
	"fmt"

	"github.com/Satyaamm/plowered/internal/core/graph"
)

// EdgeProposal is an unresolved lineage edge. Source and target are
// qualified names (as the parser saw them); converting to graph.Edge with
// real asset UUIDs requires a Resolver pass that has access to a Store.
type EdgeProposal struct {
	SourceQN          string
	TargetQN          string
	Op                Op
	TransformationID  string // FK to a Transformation row (filled by caller)
	RawSQL            string
}

// Extract turns parsed Statements into EdgeProposals. Multiple sources per
// statement become multiple proposals.
func Extract(stmts []Statement) []EdgeProposal {
	var out []EdgeProposal
	for _, s := range stmts {
		for _, src := range s.Sources {
			out = append(out, EdgeProposal{
				SourceQN: src,
				TargetQN: s.Target,
				Op:       s.Op,
				RawSQL:   s.Raw,
			})
		}
	}
	return out
}

// Resolver maps qualified-name proposals to concrete graph.Edge values by
// looking up assets through a name resolver. Unknown names are dropped or
// returned as a list of misses.
type Resolver struct {
	// LookupQN returns the asset id for a qualified name, or "" if unknown.
	LookupQN func(qn string) string
}

// Resolve converts EdgeProposals into graph.Edge values using r.LookupQN.
// Names that fail to resolve appear in `misses` rather than the edge slice.
func (r Resolver) Resolve(props []EdgeProposal) (edges []*graph.Edge, misses []string) {
	for _, p := range props {
		srcID := r.LookupQN(p.SourceQN)
		dstID := r.LookupQN(p.TargetQN)
		if srcID == "" {
			misses = append(misses, p.SourceQN)
			continue
		}
		if dstID == "" {
			misses = append(misses, p.TargetQN)
			continue
		}
		edges = append(edges, &graph.Edge{
			Kind:     graph.EdgeLineage,
			SourceID: srcID,
			TargetID: dstID,
			Properties: map[string]any{
				"op":  string(p.Op),
				"sql": truncate(p.RawSQL, 4096),
				"transformation_id": p.TransformationID,
			},
		})
	}
	return edges, misses
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("…(%d more)", len(s)-max)
}
