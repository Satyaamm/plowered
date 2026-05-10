// Package postgres_source is the Plowered adapter for customer-owned
// Postgres datasources. It currently exposes only the Tester (used by
// /v1/connections/{id}/test); the schema-crawler and column-introspector
// land in step 1.5.
//
// Config shape:
//   {
//     "host":     "db.acme.com",
//     "port":     5432,
//     "database": "warehouse",
//     "user":     "plowered_reader",
//     "sslmode":  "require"     // optional, defaults to "disable" in dev
//   }
//
// The password (and any optional channel-binding token) lives in the
// secrets vault under the connection's URN.
package postgres_source

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Satyaamm/plowered/internal/core/connection"
)

// Tester satisfies connection.Tester. Reuses pgx so the same library
// powering Plowered's own Postgres pool talks to customer Postgres.
type Tester struct{}

func New() *Tester { return &Tester{} }

func (Tester) Test(ctx context.Context, cfg map[string]any, secret []byte) error {
	dsn, err := BuildDSN(cfg, secret)
	if err != nil {
		return err
	}
	// Cap the test at 5s — a slow handshake means firewall / wrong host;
	// we'd rather fail fast than hold the user's UI.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close(ctx)
	if err := conn.Ping(ctx); err != nil {
		return fmt.Errorf("ping: %w", err)
	}
	return nil
}

// BuildDSN composes a libpq-compatible URL from the JSON config + secret.
// Exposed so the crawler and pipeline task executors can reuse it
// (one DSN-builder, no drift).
func BuildDSN(cfg map[string]any, secret []byte) (string, error) {
	host, _ := cfg["host"].(string)
	if host == "" {
		return "", errors.New("postgres_source: host is required")
	}
	port := 5432
	switch p := cfg["port"].(type) {
	case float64:
		port = int(p)
	case int:
		port = p
	}
	database, _ := cfg["database"].(string)
	if database == "" {
		return "", errors.New("postgres_source: database is required")
	}
	user, _ := cfg["user"].(string)
	if user == "" {
		return "", errors.New("postgres_source: user is required")
	}
	sslmode, _ := cfg["sslmode"].(string)
	if sslmode == "" {
		sslmode = "disable"
	}

	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(user, string(secret)),
		Host:   fmt.Sprintf("%s:%d", host, port),
		Path:   "/" + database,
	}
	q := u.Query()
	q.Set("sslmode", sslmode)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// _ ensures Tester satisfies the interface at compile time.
var _ connection.Tester = (*Tester)(nil)
