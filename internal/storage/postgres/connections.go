package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/connection"
)

// ConnectionStore satisfies connection.Repo against the connections
// table (migration 0004). Health updates are isolated to a small
// UPDATE that won't conflict with concurrent edits to config/name.
type ConnectionStore struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewConnectionStore(pool *pgxpool.Pool) *ConnectionStore {
	return &ConnectionStore{pool: pool, now: time.Now}
}

func (s *ConnectionStore) Create(ctx context.Context, c *connection.Connection) (*connection.Connection, error) {
	cp := *c
	if cp.Health == "" {
		cp.Health = connection.HealthUnknown
	}
	cfg, _ := json.Marshal(cp.Config)
	if len(cfg) == 0 || string(cfg) == "null" {
		cfg = []byte(`{}`)
	}
	const q = `
		INSERT INTO connections (tenant_id, name, type, config, secret_urn, health, created_by)
		VALUES ($1::uuid, $2, $3, $4::jsonb, $5, $6, $7::uuid)
		RETURNING id::text, created_at, updated_at`
	if err := s.pool.QueryRow(ctx, q,
		cp.TenantID, cp.Name, string(cp.Type), cfg, cp.SecretURN, string(cp.Health), cp.CreatedBy,
	).Scan(&cp.ID, &cp.CreatedAt, &cp.UpdatedAt); err != nil {
		if isUniqueViolation(err) {
			return nil, connection.ErrNameTaken
		}
		return nil, fmt.Errorf("connection: create: %w", err)
	}
	return &cp, nil
}

func (s *ConnectionStore) Update(ctx context.Context, c *connection.Connection) (*connection.Connection, error) {
	cfg, _ := json.Marshal(c.Config)
	if len(cfg) == 0 || string(cfg) == "null" {
		cfg = []byte(`{}`)
	}
	const q = `
		UPDATE connections
		   SET name = $3, type = $4, config = $5::jsonb,
		       secret_urn = $6, updated_at = NOW()
		 WHERE tenant_id = $1::uuid AND id = $2::uuid
		RETURNING updated_at`
	cp := *c
	if err := s.pool.QueryRow(ctx, q,
		cp.TenantID, cp.ID, cp.Name, string(cp.Type), cfg, cp.SecretURN,
	).Scan(&cp.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, connection.ErrNotFound
		}
		if isUniqueViolation(err) {
			return nil, connection.ErrNameTaken
		}
		return nil, fmt.Errorf("connection: update: %w", err)
	}
	return &cp, nil
}

func (s *ConnectionStore) Get(ctx context.Context, tenantID, id string) (*connection.Connection, error) {
	row := s.pool.QueryRow(ctx, selectConnectionSQL+`
		WHERE tenant_id = $1::uuid AND id = $2::uuid`, tenantID, id)
	return scanConnection(row)
}

func (s *ConnectionStore) List(ctx context.Context, tenantID string) ([]*connection.Connection, error) {
	rows, err := s.pool.Query(ctx, selectConnectionSQL+`
		WHERE tenant_id = $1::uuid
		 ORDER BY created_at DESC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("connection: list: %w", err)
	}
	defer rows.Close()
	out := []*connection.Connection{}
	for rows.Next() {
		c, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *ConnectionStore) Delete(ctx context.Context, tenantID, id string) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM connections WHERE tenant_id = $1::uuid AND id = $2::uuid`,
		tenantID, id)
	if err != nil {
		return fmt.Errorf("connection: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return connection.ErrNotFound
	}
	return nil
}

func (s *ConnectionStore) UpdateHealth(ctx context.Context, tenantID, id string, h connection.Health, at time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE connections
		   SET health = $3, last_check_at = $4, updated_at = NOW()
		 WHERE tenant_id = $1::uuid AND id = $2::uuid`,
		tenantID, id, string(h), at)
	if err != nil {
		return fmt.Errorf("connection: update health: %w", err)
	}
	return nil
}

const selectConnectionSQL = `
	SELECT id::text, tenant_id::text, name, type, config, secret_urn,
	       health,
	       COALESCE(last_check_at, '0001-01-01 00:00:00+00'::timestamptz),
	       created_by::text, created_at, updated_at
	  FROM connections`

func scanConnection(row rowScanner) (*connection.Connection, error) {
	var (
		c      connection.Connection
		cfgRaw []byte
		typ    string
		health string
	)
	if err := row.Scan(
		&c.ID, &c.TenantID, &c.Name, &typ, &cfgRaw, &c.SecretURN,
		&health, &c.LastCheckAt, &c.CreatedBy, &c.CreatedAt, &c.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, connection.ErrNotFound
		}
		return nil, err
	}
	c.Type = connection.Type(typ)
	c.Health = connection.Health(health)
	if len(cfgRaw) > 0 {
		_ = json.Unmarshal(cfgRaw, &c.Config)
	}
	return &c, nil
}
