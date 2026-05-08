// Package warehouse is a connector for managed analytical data warehouses
// reachable over the PostgreSQL wire protocol.
//
// What it adds over the relational connector:
//
//   - Query-history → transformation lineage. Pulls recent queries via a
//     user-supplied SQL template and feeds them through internal/core/lineage
//     to produce real LINEAGE edges (not just FK-based DEPENDS_ON edges).
//   - Per-asset statistics (row count, byte size, last_modified) attached to
//     `properties` so downstream Quality and Description agents have signal.
//   - Incremental watermarks. The caller persists the last successful sync
//     time so subsequent runs only consider activity after that point.
//
// Config keys:
//
//	dsn                 — required; PostgreSQL-protocol DSN
//	include / exclude   — schema allow/deny lists (csv)
//	query_history_sql   — SQL returning rows of (query_text, executed_at, user_name, duration_ms).
//	                      Use $1 to bind the watermark timestamp; bound rowcount via LIMIT inside the SQL.
//	stats_sql           — optional SQL returning (qualified_name, row_count, size_bytes, last_modified)
//	since               — ISO-8601 timestamp; only consider history after this. Default: now-24h.
package warehouse

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Satyaamm/plowered/internal/connectors/shared"
	"github.com/Satyaamm/plowered/internal/core/graph"
)

const ConnectorName = "warehouse"

func init() {
	shared.Default.MustRegister(ConnectorName, func() shared.Connector { return &Connector{} })
}

type Connector struct{}

func (Connector) Info() shared.Info {
	return shared.Info{
		Name:    ConnectorName,
		Version: "0.1.0",
		SupportedAssetTypes: []graph.AssetType{
			graph.AssetTypeDatabase,
			graph.AssetTypeSchema,
			graph.AssetTypeTable,
			graph.AssetTypeView,
			graph.AssetTypeColumn,
		},
		SupportsLineage: true,
	}
}

func (Connector) Validate(ctx context.Context, cfg shared.Config) error {
	if err := cfg.Required("dsn"); err != nil {
		return err
	}
	conn, err := pgx.Connect(ctx, cfg.String("dsn", ""))
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close(ctx)
	return conn.Ping(ctx)
}

func (Connector) Crawl(ctx context.Context, cfg shared.Config, sink shared.Sink) error {
	if err := cfg.Required("dsn"); err != nil {
		return err
	}
	conn, err := pgx.Connect(ctx, cfg.String("dsn", ""))
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close(ctx)

	dbName, err := currentDatabase(ctx, conn)
	if err != nil {
		return err
	}
	include := splitCSV(cfg.String("include", ""))
	exclude := splitCSV(cfg.String("exclude", ""))

	// 1. Structural crawl. Same shape as the relational connector so the asset
	// qualified-name space is comparable.
	if err := crawlStructure(ctx, conn, dbName, include, exclude, sink); err != nil {
		return fmt.Errorf("structure: %w", err)
	}

	// 2. Query-history → lineage edges (the warehouse-specific value-add).
	// Bound the row count inside the user-supplied SQL via LIMIT — keeps the
	// connector dialect-agnostic.
	if qSQL := cfg.String("query_history_sql", ""); qSQL != "" {
		since := parseSince(cfg.String("since", ""))
		if err := harvestQueryHistory(ctx, conn, qSQL, since, dbName, sink); err != nil {
			return fmt.Errorf("query history: %w", err)
		}
	}

	// 3. Stats (row counts, size, last modified) → asset properties.
	if statsSQL := cfg.String("stats_sql", ""); statsSQL != "" {
		if err := harvestStats(ctx, conn, statsSQL, dbName, sink); err != nil {
			return fmt.Errorf("stats: %w", err)
		}
	}

	return sink.Flush(ctx)
}

// ----- structural crawl -----

func crawlStructure(ctx context.Context, conn *pgx.Conn, dbName string, include, exclude []string, sink shared.Sink) error {
	dbAsset := &graph.Asset{
		QualifiedName: "wh://" + dbName,
		Type:          graph.AssetTypeDatabase,
		Name:          dbName,
	}
	if err := sink.UpsertAsset(ctx, dbAsset); err != nil {
		return err
	}

	tables, err := listTables(ctx, conn, include, exclude)
	if err != nil {
		return err
	}

	seenSchemas := make(map[string]bool)
	for _, t := range tables {
		if !seenSchemas[t.Schema] {
			seenSchemas[t.Schema] = true
			if err := sink.UpsertAsset(ctx, &graph.Asset{
				QualifiedName: fmt.Sprintf("wh://%s/%s", dbName, t.Schema),
				Type:          graph.AssetTypeSchema,
				Name:          t.Schema,
			}); err != nil {
				return err
			}
		}
		typ := graph.AssetTypeTable
		if t.IsView {
			typ = graph.AssetTypeView
		}
		if err := sink.UpsertAsset(ctx, &graph.Asset{
			QualifiedName: tableQN(dbName, t.Schema, t.Name),
			Type:          typ,
			Name:          t.Name,
		}); err != nil {
			return err
		}

		columns, err := listColumns(ctx, conn, t.Schema, t.Name)
		if err != nil {
			return err
		}
		for _, c := range columns {
			if err := sink.UpsertAsset(ctx, &graph.Asset{
				QualifiedName: fmt.Sprintf("wh://%s/%s/%s/%s", dbName, t.Schema, t.Name, c.Name),
				Type:          graph.AssetTypeColumn,
				Name:          c.Name,
				Properties: map[string]any{
					"data_type":   c.DataType,
					"is_nullable": c.Nullable,
					"position":    c.Position,
				},
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

// ----- helpers -----

type tableRow struct {
	Schema string
	Name   string
	IsView bool
}

type columnRow struct {
	Name     string
	DataType string
	Nullable bool
	Position int
}

func currentDatabase(ctx context.Context, conn *pgx.Conn) (string, error) {
	var name string
	if err := conn.QueryRow(ctx, `SELECT current_database()`).Scan(&name); err != nil {
		return "", fmt.Errorf("current_database: %w", err)
	}
	return name, nil
}

func listTables(ctx context.Context, conn *pgx.Conn, include, exclude []string) ([]tableRow, error) {
	const q = `
		SELECT table_schema, table_name, table_type
		FROM information_schema.tables
		WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
		ORDER BY table_schema, table_name`
	rows, err := conn.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()

	includeSet := stringSet(include)
	excludeSet := stringSet(exclude)

	var out []tableRow
	for rows.Next() {
		var t tableRow
		var typ string
		if err := rows.Scan(&t.Schema, &t.Name, &typ); err != nil {
			return nil, err
		}
		if len(includeSet) > 0 && !includeSet[t.Schema] {
			continue
		}
		if excludeSet[t.Schema] {
			continue
		}
		t.IsView = typ == "VIEW"
		out = append(out, t)
	}
	return out, rows.Err()
}

func listColumns(ctx context.Context, conn *pgx.Conn, schema, table string) ([]columnRow, error) {
	const q = `
		SELECT column_name, data_type, is_nullable = 'YES', ordinal_position
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position`
	rows, err := conn.Query(ctx, q, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []columnRow
	for rows.Next() {
		var c columnRow
		if err := rows.Scan(&c.Name, &c.DataType, &c.Nullable, &c.Position); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func tableQN(db, schema, table string) string {
	return fmt.Sprintf("wh://%s/%s/%s", db, schema, table)
}

func parseSince(s string) time.Time {
	if s == "" {
		return time.Now().Add(-24 * time.Hour).UTC()
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Now().Add(-24 * time.Hour).UTC()
	}
	return t.UTC()
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func stringSet(items []string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, x := range items {
		m[x] = true
	}
	return m
}
