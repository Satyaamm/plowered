package warehouse

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Satyaamm/plowered/internal/connectors/shared"
	"github.com/Satyaamm/plowered/internal/core/graph"
)

// StatsRow is the contract for the configured stats_sql. Columns must arrive
// in this order:
//
//	qualified_name TEXT          -- "schema.table"; bare table assumes 'public'
//	row_count      BIGINT
//	size_bytes     BIGINT
//	last_modified  TIMESTAMPTZ
type StatsRow struct {
	QualifiedName string
	RowCount      int64
	SizeBytes     int64
	LastModified  time.Time
}

// harvestStats runs stats_sql, normalizes each row to the warehouse QN
// scheme, and re-upserts the asset with the stats merged into properties.
// Existing description / tags / owners are preserved by the BatchedSink's
// upsert-then-update path.
func harvestStats(ctx context.Context, conn *pgx.Conn, sql, dbName string, sink shared.Sink) error {
	rows, err := conn.Query(ctx, sql)
	if err != nil {
		return fmt.Errorf("execute stats_sql: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var s StatsRow
		if err := rows.Scan(&s.QualifiedName, &s.RowCount, &s.SizeBytes, &s.LastModified); err != nil {
			return fmt.Errorf("scan stats row: %w", err)
		}
		qn := normalizeQN(dbName, s.QualifiedName)
		asset := &graph.Asset{
			QualifiedName: qn,
			Type:          graph.AssetTypeTable,
			Name:          lastSegment(qn),
			Properties: map[string]any{
				"row_count":     s.RowCount,
				"size_bytes":    s.SizeBytes,
				"last_modified": s.LastModified.UTC().Format(time.RFC3339),
			},
		}
		if err := sink.UpsertAsset(ctx, asset); err != nil {
			return err
		}
	}
	return rows.Err()
}

func lastSegment(qn string) string {
	if qn == "" {
		return ""
	}
	last := 0
	for i, r := range qn {
		if r == '/' {
			last = i + 1
		}
	}
	if last >= len(qn) {
		return qn
	}
	return qn[last:]
}
