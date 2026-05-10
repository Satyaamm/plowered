package postgres_source

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Satyaamm/plowered/internal/core/crawler"
)

// Crawler is the Postgres adapter's information_schema reader. It
// satisfies crawler.Source and is what the worker calls when a
// /v1/connections/{id}/crawl job runs.
//
// Two design choices worth flagging:
//
//   1. We exclude the standard system schemas (information_schema +
//      pg_*). Surfacing pg_catalog in a customer's catalog UI is noise.
//   2. We pull the entire tree (schemas + tables + columns) in three
//      queries instead of N+1. With 50K columns this is ~50ms over a
//      LAN; pagination + streaming is a problem we'll inherit when we
//      hit a 500K-column warehouse, not before.
type Crawler struct{}

func NewCrawler() *Crawler { return &Crawler{} }

// Crawl implements crawler.Source.
func (Crawler) Crawl(ctx context.Context, cfg map[string]any, secret []byte) (*crawler.Tree, error) {
	dsn, err := BuildDSN(cfg, secret)
	if err != nil {
		return nil, err
	}
	// Cap the entire crawl at 60s. A long crawl usually means
	// either the source isn't reachable or its information_schema is
	// pathologically large; bailing out keeps the worker queue moving.
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer conn.Close(ctx)

	schemas, err := listSchemas(ctx, conn)
	if err != nil {
		return nil, err
	}
	tables, err := listTables(ctx, conn)
	if err != nil {
		return nil, err
	}
	columns, err := listColumns(ctx, conn)
	if err != nil {
		return nil, err
	}

	// Build the tree by stitching the three lists together.
	type tableKey struct{ schema, name string }
	colsByTable := map[tableKey][]crawler.ColumnInfo{}
	for _, c := range columns {
		k := tableKey{c.schema, c.table}
		colsByTable[k] = append(colsByTable[k], c.info)
	}
	for k := range colsByTable {
		sort.Slice(colsByTable[k], func(i, j int) bool {
			return colsByTable[k][i].OrdinalPos < colsByTable[k][j].OrdinalPos
		})
	}

	tablesBySchema := map[string][]crawler.TableInfo{}
	for _, t := range tables {
		ti := crawler.TableInfo{
			Name:        t.name,
			Kind:        t.kind,
			Description: t.description,
			Columns:     colsByTable[tableKey{t.schema, t.name}],
		}
		tablesBySchema[t.schema] = append(tablesBySchema[t.schema], ti)
	}

	tree := &crawler.Tree{}
	for _, name := range schemas {
		tree.Schemas = append(tree.Schemas, crawler.SchemaInfo{
			Name:   name,
			Tables: tablesBySchema[name],
		})
	}
	return tree, nil
}

func listSchemas(ctx context.Context, conn *pgx.Conn) ([]string, error) {
	const q = `
		SELECT schema_name
		  FROM information_schema.schemata
		 WHERE schema_name NOT IN ('information_schema', 'pg_catalog', 'pg_toast')
		   AND schema_name NOT LIKE 'pg_temp_%'
		   AND schema_name NOT LIKE 'pg_toast_temp_%'
		 ORDER BY schema_name`
	rows, err := conn.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list schemas: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

type tableRow struct {
	schema      string
	name        string
	kind        string
	description string
}

func listTables(ctx context.Context, conn *pgx.Conn) ([]tableRow, error) {
	const q = `
		SELECT t.table_schema,
		       t.table_name,
		       LOWER(t.table_type),
		       COALESCE(obj_description(c.oid), '') AS description
		  FROM information_schema.tables t
		  LEFT JOIN pg_class c
		    ON c.relname = t.table_name
		   AND c.relnamespace = (SELECT oid FROM pg_namespace WHERE nspname = t.table_schema)
		 WHERE t.table_schema NOT IN ('information_schema', 'pg_catalog', 'pg_toast')
		   AND t.table_schema NOT LIKE 'pg_temp_%'
		   AND t.table_schema NOT LIKE 'pg_toast_temp_%'
		 ORDER BY t.table_schema, t.table_name`
	rows, err := conn.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer rows.Close()
	out := []tableRow{}
	for rows.Next() {
		var r tableRow
		if err := rows.Scan(&r.schema, &r.name, &r.kind, &r.description); err != nil {
			return nil, err
		}
		// information_schema reports "BASE TABLE" / "VIEW" — normalize.
		switch r.kind {
		case "base table":
			r.kind = "table"
		case "view":
			r.kind = "view"
		default:
			r.kind = "table"
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type columnRow struct {
	schema string
	table  string
	info   crawler.ColumnInfo
}

func listColumns(ctx context.Context, conn *pgx.Conn) ([]columnRow, error) {
	// `data_type` is the standard form ("character varying"); for
	// catalogue display we'd prefer the regular Postgres alias ("text",
	// "varchar(255)") which lives in pg_catalog. udt_name gets us close
	// enough without the pg_catalog dance.
	const q = `
		SELECT c.table_schema,
		       c.table_name,
		       c.column_name,
		       COALESCE(c.udt_name, c.data_type),
		       (c.is_nullable = 'YES'),
		       c.ordinal_position,
		       COALESCE(c.column_default, ''),
		       COALESCE(col_description(pgc.oid, c.ordinal_position::int), '')
		  FROM information_schema.columns c
		  LEFT JOIN pg_class pgc
		    ON pgc.relname = c.table_name
		   AND pgc.relnamespace = (SELECT oid FROM pg_namespace WHERE nspname = c.table_schema)
		 WHERE c.table_schema NOT IN ('information_schema', 'pg_catalog', 'pg_toast')
		   AND c.table_schema NOT LIKE 'pg_temp_%'
		   AND c.table_schema NOT LIKE 'pg_toast_temp_%'
		 ORDER BY c.table_schema, c.table_name, c.ordinal_position`
	rows, err := conn.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list columns: %w", err)
	}
	defer rows.Close()
	out := []columnRow{}
	for rows.Next() {
		var (
			r columnRow
			c crawler.ColumnInfo
		)
		if err := rows.Scan(
			&r.schema, &r.table, &c.Name, &c.DataType, &c.Nullable,
			&c.OrdinalPos, &c.Default, &c.Description,
		); err != nil {
			return nil, err
		}
		r.info = c
		out = append(out, r)
	}
	return out, rows.Err()
}

// _ ensures Crawler satisfies the interface at compile time.
var _ crawler.Source = (*Crawler)(nil)
