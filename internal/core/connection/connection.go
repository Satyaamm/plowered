// Package connection models the customer datasources Plowered talks to:
// Postgres, Snowflake, BigQuery, Redshift, Databricks. Connection rows
// are tenant-scoped; credentials live in the secrets vault and the row
// only stores the URN.
//
// The actual driver code (open a Postgres pool, run information_schema)
// lives in internal/adapters/<source>/. This package is the metadata
// surface — what's configured, who wired it, when it was last healthy.
package connection

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned by Get / List when the resource is unknown.
var ErrNotFound = errors.New("connection: not found")

// ErrNameTaken is returned when (tenant_id, name) already has a row.
var ErrNameTaken = errors.New("connection: name already used in this workspace")

// Type enumerates the data sources we know how to talk to. Adding a new
// source means two changes: append a constant here and register an
// adapter in internal/adapters/<source>/.
type Type string

const (
	TypePostgres   Type = "postgres"
	TypeSnowflake  Type = "snowflake"
	TypeBigQuery   Type = "bigquery"
	TypeRedshift   Type = "redshift"
	TypeDatabricks Type = "databricks"
	TypeMySQL      Type = "mysql"
)

// Health is the live state from the most-recent test/check.
type Health string

const (
	HealthUnknown      Health = "unknown"
	HealthHealthy      Health = "healthy"
	HealthDegraded     Health = "degraded"
	HealthUnreachable  Health = "unreachable"
)

// Connection is one configured datasource. The Config field is type-
// specific (Postgres needs host/port/database/user/sslmode; Snowflake
// needs account/warehouse/role/database/schema). Validation per-type
// lives in the adapter packages — we don't try to pre-validate it here
// because a new source should be addable without editing this file.
type Connection struct {
	ID           string
	TenantID     string
	Name         string
	Type         Type
	Config       map[string]any
	SecretURN    string         // points at the secrets vault entry
	Health       Health
	LastCheckAt  time.Time
	CreatedBy    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Repo persists connections.
type Repo interface {
	Create(ctx context.Context, c *Connection) (*Connection, error)
	Update(ctx context.Context, c *Connection) (*Connection, error)
	Get(ctx context.Context, tenantID, id string) (*Connection, error)
	List(ctx context.Context, tenantID string) ([]*Connection, error)
	Delete(ctx context.Context, tenantID, id string) error
	UpdateHealth(ctx context.Context, tenantID, id string, h Health, at time.Time) error
}

// Tester validates that a config + secret can actually reach the source.
// Implementations live in internal/adapters/<source>/ and register
// themselves with the registry below.
type Tester interface {
	Test(ctx context.Context, cfg map[string]any, secret []byte) error
}

// Registry pairs each Type with its Tester. The HTTP layer uses it to
// route /v1/connections/{id}/test by Type.
type Registry struct {
	testers map[Type]Tester
}

func NewRegistry() *Registry { return &Registry{testers: make(map[Type]Tester)} }

func (r *Registry) Register(t Type, tester Tester) { r.testers[t] = tester }

func (r *Registry) Tester(t Type) (Tester, bool) {
	tt, ok := r.testers[t]
	return tt, ok
}

// SecretURNFor builds a stable URN for a connection's credentials. The
// caller is the connection store on Create; the URN is then stamped on
// the row and used by the secrets vault on subsequent reads.
func SecretURNFor(tenantID, connectionID string) string {
	return "secret://" + tenantID + "/connection/" + connectionID
}
