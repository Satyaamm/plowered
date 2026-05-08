package warehouse

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Satyaamm/plowered/internal/connectors/shared"
	"github.com/Satyaamm/plowered/internal/core/graph"
	"github.com/Satyaamm/plowered/internal/core/lineage"
)

// HistoryRow is one entry from the warehouse's query history view. The
// configured query_history_sql must SELECT these columns in this order:
//
//	query_text  TEXT
//	executed_at TIMESTAMPTZ
//	user_name   TEXT     -- nullable; empty string if absent
//	duration_ms BIGINT   -- nullable; 0 if absent
type HistoryRow struct {
	QueryText  string
	ExecutedAt time.Time
	UserName   string
	DurationMS int64
}

// harvestQueryHistory pulls rows via the user-supplied SQL, parses each query
// through internal/core/lineage, and emits LINEAGE edges with provenance
// (sql hash + executed_at + user) attached to properties.
//
// Source/target qualified names from the parser are mapped onto the warehouse's
// "wh://<db>/<schema>/<table>" naming so they line up with the structural
// crawl. Edges with unresolvable names are skipped silently — they still show
// up as misses in the resolver pass that runs in the graph engine.
func harvestQueryHistory(ctx context.Context, conn *pgx.Conn, sql string, since time.Time, dbName string, sink shared.Sink) error {
	rows, err := conn.Query(ctx, sql, since)
	if err != nil {
		return fmt.Errorf("execute query_history_sql: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var h HistoryRow
		if err := rows.Scan(&h.QueryText, &h.ExecutedAt, &h.UserName, &h.DurationMS); err != nil {
			return fmt.Errorf("scan history row: %w", err)
		}
		if err := emitFromQuery(ctx, h, dbName, sink); err != nil {
			return err
		}
	}
	return rows.Err()
}

// emitFromQuery parses one query into Statements and emits LINEAGE edges.
// Failures to parse are non-fatal — the bad query is dropped and the run
// continues. Lineage is best-effort; one malformed query should not poison
// the entire history pull.
func emitFromQuery(ctx context.Context, h HistoryRow, dbName string, sink shared.Sink) error {
	stmts := lineage.Parse(h.QueryText)
	if len(stmts) == 0 {
		return nil
	}
	props := lineage.Extract(stmts)
	hash := sqlHash(h.QueryText)

	for _, p := range props {
		src := normalizeQN(dbName, p.SourceQN)
		tgt := normalizeQN(dbName, p.TargetQN)
		edge := &graph.Edge{
			Kind: graph.EdgeLineage,
			Properties: map[string]any{
				"source_qn":   src,
				"target_qn":   tgt,
				"op":          string(p.Op),
				"sql_hash":    hash,
				"executed_at": h.ExecutedAt.UTC().Format(time.RFC3339),
				"user":        h.UserName,
				"duration_ms": h.DurationMS,
			},
		}
		if err := sink.UpsertEdge(ctx, edge); err != nil {
			return err
		}
	}
	return nil
}

// normalizeQN converts a parser-emitted name like `mart.orders` or just
// `orders` to the warehouse qualified-name scheme used by the structural
// crawl: `wh://<db>/<schema>/<table>`. Bare names get the `public` schema.
func normalizeQN(dbName, raw string) string {
	if raw == "" {
		return ""
	}
	// Already fully qualified by the parser? Pass through unchanged so the
	// graph resolver can either find it or report a miss.
	if hasPrefix(raw, "wh://") {
		return raw
	}
	parts := splitDots(raw)
	switch len(parts) {
	case 1:
		return tableQN(dbName, "public", parts[0])
	case 2:
		return tableQN(dbName, parts[0], parts[1])
	default:
		// db.schema.table or longer — take the last three components.
		n := len(parts)
		return tableQN(parts[n-3], parts[n-2], parts[n-1])
	}
}

func hasPrefix(s, p string) bool {
	if len(s) < len(p) {
		return false
	}
	return s[:len(p)] == p
}

func splitDots(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '.' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	out = append(out, cur)
	return out
}

func sqlHash(sql string) string {
	h := sha256.Sum256([]byte(sql))
	return hex.EncodeToString(h[:])[:16]
}
