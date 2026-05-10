// Package taskdeps holds the cross-cutting dependency types that pipeline
// task executors need. Living in its own package avoids import cycles
// between executor packages and the wiring layer (cmd/) that constructs
// the concrete dialer.
package taskdeps

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// ConnFactory dials a customer-defined connection by ID and returns a live
// pgx.Conn. The caller is responsible for Close()ing the returned conn.
//
// v0 supports Postgres-typed connections only; other types should return
// an error. The factory itself loads the row from connection.Repo, reads
// the secret from secrets.Vault, and builds a DSN — see cmd/plowered for
// the wiring.
type ConnFactory func(ctx context.Context, tenantID, connectionID string) (*pgx.Conn, error)
