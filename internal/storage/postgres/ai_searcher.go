package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/connection"
	"github.com/Satyaamm/plowered/internal/core/search"
)

// AISemanticSearcher adapts the catalog's *search.Searcher to the
// asker.SemanticSearcher interface, restricting results to a single
// connection and to table-shaped assets.
//
// Scoping strategy: every catalog asset has a qualified_name; tables
// in this codebase are qualified as "<connection_name>.<schema>.<table>".
// We resolve the connection's name, then keep only Hits whose
// qualified_name starts with that prefix and whose type is 'table'.
//
// We over-fetch (k * 4) from the underlying searcher to account for
// the filter trimming results.
type AISemanticSearcher struct {
	pool     *pgxpool.Pool
	searcher *search.Searcher
	conns    connection.Repo
}

func NewAISemanticSearcher(pool *pgxpool.Pool, s *search.Searcher, conns connection.Repo) *AISemanticSearcher {
	return &AISemanticSearcher{pool: pool, searcher: s, conns: conns}
}

// TopTables returns up to k table asset IDs in this connection that
// best match the question. Empty slice (not error) when nothing
// relevant is indexed yet.
func (a *AISemanticSearcher) TopTables(ctx context.Context, tenantID, connectionID, question string, k int) ([]string, error) {
	if a.searcher == nil || a.conns == nil {
		return nil, fmt.Errorf("ai_searcher: not fully configured")
	}
	conn, err := a.conns.Get(ctx, tenantID, connectionID)
	if err != nil {
		return nil, fmt.Errorf("load connection: %w", err)
	}
	prefix := conn.Name + "."
	overFetch := k * 4
	if overFetch < 20 {
		overFetch = 20
	}
	hits, err := a.searcher.Query(ctx, tenantID, question, overFetch)
	if err != nil {
		return nil, fmt.Errorf("semantic search: %w", err)
	}
	out := make([]string, 0, k)
	for _, h := range hits {
		if h.Asset == nil {
			continue
		}
		if string(h.Asset.Type) != "table" && string(h.Asset.Type) != "view" {
			continue
		}
		if len(h.Asset.QualifiedName) < len(prefix) || h.Asset.QualifiedName[:len(prefix)] != prefix {
			continue
		}
		out = append(out, h.Asset.ID)
		if len(out) >= k {
			break
		}
	}
	return out, nil
}
