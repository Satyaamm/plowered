package postgres

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type migration struct {
	version int
	name    string
	up      string
	down    string
}

// Migrate applies all pending up-migrations in lexical order. It is safe to
// run on every startup — applied migrations are tracked in
// schema_migrations and never re-run.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER  PRIMARY KEY,
			name       TEXT     NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`); err != nil {
		return fmt.Errorf("ensure schema_migrations: %w", err)
	}

	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	applied, err := loadApplied(ctx, pool)
	if err != nil {
		return err
	}

	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		if err := applyOne(ctx, pool, m); err != nil {
			return fmt.Errorf("apply migration %d %s: %w", m.version, m.name, err)
		}
	}
	return nil
}

func applyOne(ctx context.Context, pool *pgxpool.Pool, m migration) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, m.up); err != nil {
		return fmt.Errorf("up: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`,
		m.version, m.name,
	); err != nil {
		return fmt.Errorf("record: %w", err)
	}
	return tx.Commit(ctx)
}

func loadApplied(ctx context.Context, pool *pgxpool.Pool) (map[int]bool, error) {
	rows, err := pool.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()

	out := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = true
	}
	return out, rows.Err()
}

func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}

	bucket := make(map[int]*migration)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		v, name, dir, err := parseMigrationName(e.Name())
		if err != nil {
			return nil, err
		}
		body, err := fs.ReadFile(migrationsFS, "migrations/"+e.Name())
		if err != nil {
			return nil, err
		}
		m := bucket[v]
		if m == nil {
			m = &migration{version: v, name: name}
			bucket[v] = m
		}
		switch dir {
		case "up":
			m.up = string(body)
		case "down":
			m.down = string(body)
		}
	}

	out := make([]migration, 0, len(bucket))
	for _, m := range bucket {
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].version < out[j].version })
	return out, nil
}

// parseMigrationName extracts version, descriptive name, and direction from
// a filename of the form `0001_init.up.sql` / `0001_init.down.sql`.
func parseMigrationName(filename string) (version int, name, dir string, err error) {
	base := strings.TrimSuffix(filename, ".sql")
	parts := strings.SplitN(base, ".", 2)
	if len(parts) != 2 {
		return 0, "", "", fmt.Errorf("malformed migration name: %s", filename)
	}
	dir = parts[1]
	if dir != "up" && dir != "down" {
		return 0, "", "", fmt.Errorf("migration direction must be up|down: %s", filename)
	}

	stem := parts[0]
	idx := strings.Index(stem, "_")
	if idx < 1 {
		return 0, "", "", fmt.Errorf("migration name needs version prefix: %s", filename)
	}
	v, convErr := strconv.Atoi(stem[:idx])
	if convErr != nil {
		return 0, "", "", fmt.Errorf("migration version not numeric: %s", filename)
	}
	return v, stem[idx+1:], dir, nil
}
