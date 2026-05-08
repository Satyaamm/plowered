// Package relational is a connector for relational databases reachable via
// the SQL standard `information_schema`. It crawls schemas, tables, views,
// columns, and emits FK relationships as DEPENDS_ON edges.
//
// Config keys:
//
//	dsn          — connection string, e.g. postgres://user:pass@host:5432/db
//	include      — optional comma-separated schema allow-list
//	exclude      — optional comma-separated schema deny-list
//	with_lineage — bool; emit FK-based DEPENDS_ON edges (default true)
package relational

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/Satyaamm/plowered/internal/connectors/shared"
	"github.com/Satyaamm/plowered/internal/core/graph"
)

const ConnectorName = "relational"

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

	dbAsset := &graph.Asset{
		QualifiedName: "rel://" + dbName,
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
			schemaAsset := &graph.Asset{
				QualifiedName: fmt.Sprintf("rel://%s/%s", dbName, t.Schema),
				Type:          graph.AssetTypeSchema,
				Name:          t.Schema,
			}
			if err := sink.UpsertAsset(ctx, schemaAsset); err != nil {
				return err
			}
		}
		assetType := graph.AssetTypeTable
		if t.IsView {
			assetType = graph.AssetTypeView
		}
		tableAsset := &graph.Asset{
			QualifiedName: fmt.Sprintf("rel://%s/%s/%s", dbName, t.Schema, t.Name),
			Type:          assetType,
			Name:          t.Name,
		}
		if err := sink.UpsertAsset(ctx, tableAsset); err != nil {
			return err
		}

		columns, err := listColumns(ctx, conn, t.Schema, t.Name)
		if err != nil {
			return err
		}
		for _, c := range columns {
			colAsset := &graph.Asset{
				QualifiedName: fmt.Sprintf("rel://%s/%s/%s/%s", dbName, t.Schema, t.Name, c.Name),
				Type:          graph.AssetTypeColumn,
				Name:          c.Name,
				Properties: map[string]any{
					"data_type":  c.DataType,
					"is_nullable": c.Nullable,
					"position":   c.Position,
				},
			}
			if err := sink.UpsertAsset(ctx, colAsset); err != nil {
				return err
			}
		}
	}

	if cfg.Bool("with_lineage", true) {
		if err := emitFKEdges(ctx, conn, dbName, sink); err != nil {
			return err
		}
	}
	return sink.Flush(ctx)
}

// ----- DB queries -----

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
	q := `
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
	q := `
		SELECT column_name, data_type, is_nullable = 'YES', ordinal_position
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position`
	rows, err := conn.Query(ctx, q, schema, table)
	if err != nil {
		return nil, fmt.Errorf("list columns %s.%s: %w", schema, table, err)
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

func emitFKEdges(ctx context.Context, conn *pgx.Conn, dbName string, sink shared.Sink) error {
	const q = `
		SELECT
			tc.table_schema AS from_schema,
			tc.table_name   AS from_table,
			ccu.table_schema AS to_schema,
			ccu.table_name   AS to_table
		FROM information_schema.table_constraints tc
		JOIN information_schema.constraint_column_usage ccu
		  ON tc.constraint_name = ccu.constraint_name
		 AND tc.table_schema    = ccu.constraint_schema
		WHERE tc.constraint_type = 'FOREIGN KEY'
		  AND tc.table_schema NOT IN ('pg_catalog', 'information_schema')`

	rows, err := conn.Query(ctx, q)
	if err != nil {
		return fmt.Errorf("list fks: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var fromSchema, fromTable, toSchema, toTable string
		if err := rows.Scan(&fromSchema, &fromTable, &toSchema, &toTable); err != nil {
			return err
		}
		// Edge stores qualified-name pointers; the resolution to UUIDs happens
		// inside the Sink (it looks up the assets it just upserted by QN).
		// For v0 the BatchedSink does not auto-resolve; we emit a structural
		// hint via Properties and leave UUID-based edge creation to a later
		// resolver pass.
		edge := &graph.Edge{
			Kind: graph.EdgeDependsOn,
			Properties: map[string]any{
				"source_qn": fmt.Sprintf("rel://%s/%s/%s", dbName, fromSchema, fromTable),
				"target_qn": fmt.Sprintf("rel://%s/%s/%s", dbName, toSchema, toTable),
				"reason":    "foreign_key",
			},
		}
		if err := sink.UpsertEdge(ctx, edge); err != nil {
			return err
		}
	}
	return rows.Err()
}

// ----- helpers -----

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
