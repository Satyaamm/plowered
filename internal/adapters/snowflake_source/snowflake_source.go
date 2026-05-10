// Package snowflake_source is the Plowered adapter for customer-owned
// Snowflake datasources. It uses database/sql so the actual driver
// (gosnowflake) is decoupled from this package — the cmd binary blank-
// imports it. This keeps the rest of the codebase compiling even when
// the deployment doesn't ship the Snowflake driver.
//
// Config shape (JSON):
//
//	{
//	  "account":   "xy12345.us-east-1",  // required: <orgname>-<account>
//	  "user":      "PLOWERED_READER",     // required
//	  "warehouse": "ANALYTICS_WH",        // optional but recommended
//	  "role":      "PUBLIC",              // optional
//	  "database":  "RAW",                  // optional; crawl scopes to it
//	  "schema":    "PUBLIC",               // optional; crawl scopes to it
//	  "authenticator": "snowflake",       // or "externalbrowser" / "oauth"
//	  "passcode": "",                      // MFA passcode if applicable
//	}
//
// The password (or OAuth token) lives in the secrets vault under the
// connection's URN.
//
// To enable real connections, the cmd binary must blank-import the
// driver so it registers itself with database/sql:
//
//	import _ "github.com/snowflakedb/gosnowflake"
package snowflake_source

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/Satyaamm/plowered/internal/core/connection"
	"github.com/Satyaamm/plowered/internal/core/crawler"
)

// driverName matches whatever the gosnowflake driver registers itself
// as. We never compare against this externally; sql.Open() does.
const driverName = "snowflake"

// Tester satisfies connection.Tester. The Ping is a real round-trip so
// we exercise network reachability + credential validity.
type Tester struct{}

func New() *Tester { return &Tester{} }

func (Tester) Test(ctx context.Context, cfg map[string]any, secret []byte) error {
	dsn, err := BuildDSN(cfg, secret)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	return nil
}

// Crawler walks the Snowflake INFORMATION_SCHEMA. Scopes:
//   - If cfg.database is set, crawls only that database.
//   - If cfg.schema is also set, scopes further to that schema.
//   - Otherwise enumerates SHOW DATABASES (excluding the standard system
//     schemas SNOWFLAKE / SNOWFLAKE_SAMPLE_DATA).
type Crawler struct{}

func NewCrawler() *Crawler { return &Crawler{} }

func (Crawler) Crawl(ctx context.Context, cfg map[string]any, secret []byte) (*crawler.Tree, error) {
	dsn, err := BuildDSN(cfg, secret)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}
	defer db.Close()

	scopedDB, _ := cfg["database"].(string)
	scopedSchema, _ := cfg["schema"].(string)

	databases, err := listDatabases(ctx, db, scopedDB)
	if err != nil {
		return nil, err
	}

	tree := &crawler.Tree{}
	for _, dbName := range databases {
		schemas, err := listSchemas(ctx, db, dbName, scopedSchema)
		if err != nil {
			return nil, err
		}
		tables, err := listTables(ctx, db, dbName, scopedSchema)
		if err != nil {
			return nil, err
		}
		columns, err := listColumns(ctx, db, dbName, scopedSchema)
		if err != nil {
			return nil, err
		}

		type tk struct{ schema, name string }
		colsByTable := map[tk][]crawler.ColumnInfo{}
		for _, c := range columns {
			k := tk{c.schema, c.table}
			colsByTable[k] = append(colsByTable[k], c.info)
		}
		for k := range colsByTable {
			sort.Slice(colsByTable[k], func(i, j int) bool {
				return colsByTable[k][i].OrdinalPos < colsByTable[k][j].OrdinalPos
			})
		}

		tablesBySchema := map[string][]crawler.TableInfo{}
		for _, t := range tables {
			tablesBySchema[t.schema] = append(tablesBySchema[t.schema], crawler.TableInfo{
				Name:        t.name,
				Kind:        t.kind,
				Description: t.description,
				Columns:     colsByTable[tk{t.schema, t.name}],
			})
		}

		for _, s := range schemas {
			// Prefix schemas with the database name so multi-database
			// catalogues never collide on schema names like "PUBLIC".
			tree.Schemas = append(tree.Schemas, crawler.SchemaInfo{
				Name:   dbName + "." + s,
				Tables: tablesBySchema[s],
			})
		}
	}
	return tree, nil
}

// BuildDSN composes the gosnowflake DSN. Exposed so pipeline executors
// (sql_run / transform_run / connector_sync) can reuse it.
//
// gosnowflake DSN shape:
//
//	user:password@account/database/schema?param1=value1&...
//
// (https://pkg.go.dev/github.com/snowflakedb/gosnowflake#hdr-Connection_String)
func BuildDSN(cfg map[string]any, secret []byte) (string, error) {
	account, _ := cfg["account"].(string)
	if account == "" {
		return "", errors.New("snowflake_source: account is required (e.g. xy12345.us-east-1)")
	}
	user, _ := cfg["user"].(string)
	if user == "" {
		return "", errors.New("snowflake_source: user is required")
	}
	db, _ := cfg["database"].(string)
	schema, _ := cfg["schema"].(string)
	role, _ := cfg["role"].(string)
	warehouse, _ := cfg["warehouse"].(string)
	authenticator, _ := cfg["authenticator"].(string)
	if authenticator == "" {
		authenticator = "snowflake"
	}

	var b strings.Builder
	b.WriteString(url.QueryEscape(user))
	if len(secret) > 0 && authenticator == "snowflake" {
		b.WriteString(":")
		b.WriteString(url.QueryEscape(string(secret)))
	}
	b.WriteString("@")
	b.WriteString(account)
	if db != "" {
		b.WriteString("/")
		b.WriteString(db)
		if schema != "" {
			b.WriteString("/")
			b.WriteString(schema)
		}
	}
	q := url.Values{}
	if warehouse != "" {
		q.Set("warehouse", warehouse)
	}
	if role != "" {
		q.Set("role", role)
	}
	if authenticator != "snowflake" {
		q.Set("authenticator", authenticator)
		if pass, ok := cfg["passcode"].(string); ok && pass != "" {
			q.Set("passcode", pass)
		}
	}
	if len(q) > 0 {
		b.WriteString("?")
		b.WriteString(q.Encode())
	}
	return b.String(), nil
}

// listDatabases enumerates the user-visible databases or scopes to a
// single one. SHOW DATABASES is preferred over INFORMATION_SCHEMA so we
// don't have to query a database without knowing what's there.
func listDatabases(ctx context.Context, db *sql.DB, scope string) ([]string, error) {
	if scope != "" {
		return []string{scope}, nil
	}
	rows, err := db.QueryContext(ctx, "SHOW DATABASES")
	if err != nil {
		return nil, fmt.Errorf("show databases: %w", err)
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	// SHOW DATABASES returns ~15 columns; we only want "name" (col 1).
	var out []string
	for rows.Next() {
		raw := make([]any, len(cols))
		dest := make([]any, len(cols))
		for i := range raw {
			dest[i] = &raw[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, err
		}
		name, _ := raw[1].(string)
		if name == "" || name == "SNOWFLAKE" || name == "SNOWFLAKE_SAMPLE_DATA" {
			continue
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

func listSchemas(ctx context.Context, db *sql.DB, dbName, scopedSchema string) ([]string, error) {
	if scopedSchema != "" {
		return []string{scopedSchema}, nil
	}
	q := fmt.Sprintf(`
		SELECT SCHEMA_NAME
		  FROM %s.INFORMATION_SCHEMA.SCHEMATA
		 WHERE SCHEMA_NAME <> 'INFORMATION_SCHEMA'
		 ORDER BY SCHEMA_NAME`, dbName)
	rows, err := db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list schemas (%s): %w", dbName, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

type sfTable struct {
	schema, name, kind, description string
}

func listTables(ctx context.Context, db *sql.DB, dbName, scopedSchema string) ([]sfTable, error) {
	q := fmt.Sprintf(`
		SELECT TABLE_SCHEMA, TABLE_NAME, LOWER(TABLE_TYPE), COALESCE(COMMENT, '')
		  FROM %s.INFORMATION_SCHEMA.TABLES
		 WHERE TABLE_SCHEMA <> 'INFORMATION_SCHEMA'`, dbName)
	args := []any{}
	if scopedSchema != "" {
		q += " AND TABLE_SCHEMA = ?"
		args = append(args, scopedSchema)
	}
	q += " ORDER BY TABLE_SCHEMA, TABLE_NAME"
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list tables (%s): %w", dbName, err)
	}
	defer rows.Close()
	out := []sfTable{}
	for rows.Next() {
		var r sfTable
		if err := rows.Scan(&r.schema, &r.name, &r.kind, &r.description); err != nil {
			return nil, err
		}
		switch r.kind {
		case "base table":
			r.kind = "table"
		case "view":
			r.kind = "view"
		case "external table":
			r.kind = "external_table"
		default:
			r.kind = "table"
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type sfColumn struct {
	schema, table string
	info          crawler.ColumnInfo
}

func listColumns(ctx context.Context, db *sql.DB, dbName, scopedSchema string) ([]sfColumn, error) {
	q := fmt.Sprintf(`
		SELECT TABLE_SCHEMA, TABLE_NAME, COLUMN_NAME, DATA_TYPE,
		       (IS_NULLABLE = 'YES'),
		       ORDINAL_POSITION,
		       COALESCE(COLUMN_DEFAULT, ''),
		       COALESCE(COMMENT, '')
		  FROM %s.INFORMATION_SCHEMA.COLUMNS
		 WHERE TABLE_SCHEMA <> 'INFORMATION_SCHEMA'`, dbName)
	args := []any{}
	if scopedSchema != "" {
		q += " AND TABLE_SCHEMA = ?"
		args = append(args, scopedSchema)
	}
	q += " ORDER BY TABLE_SCHEMA, TABLE_NAME, ORDINAL_POSITION"
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list columns (%s): %w", dbName, err)
	}
	defer rows.Close()
	out := []sfColumn{}
	for rows.Next() {
		var (
			r sfColumn
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

var _ connection.Tester = (*Tester)(nil)
var _ crawler.Source = (*Crawler)(nil)
