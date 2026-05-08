// Package postgres is the production Store implementation. It enforces
// tenant isolation on every query by deriving tenant_id from ctx — handlers
// cannot pass it in. See SECURITY.md §4 (Multi-tenancy).
package postgres

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/graph"
	"github.com/Satyaamm/plowered/internal/storage"
)

// Store is the PostgreSQL-backed implementation of storage.Store.
type Store struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

// New constructs a Store from a pgx connection pool. The caller owns the
// pool lifecycle.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, now: time.Now}
}

func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

// Close is a no-op; pool ownership belongs to the caller.
func (s *Store) Close() error { return nil }

func (s *Store) CreateAsset(ctx context.Context, a *graph.Asset) (*graph.Asset, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, graph.ErrInvalidArgument
	}
	cp := *a
	cp.TenantID = tenant
	if cp.ID == "" {
		cp.ID = newUUID()
	}
	now := s.now().UTC()
	cp.CreatedAt = now
	cp.UpdatedAt = now
	if cp.Trust == "" {
		cp.Trust = graph.TrustUnverified
	}

	tagsJSON, ownersJSON, propsJSON, err := encodeAssetJSON(&cp)
	if err != nil {
		return nil, err
	}

	const q = `
		INSERT INTO assets (
			id, tenant_id, qualified_name, type, name,
			description, description_ai, trust,
			tags, owners, properties,
			created_at, updated_at, created_by, updated_by
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`
	_, err = s.pool.Exec(ctx, q,
		cp.ID, cp.TenantID, cp.QualifiedName, string(cp.Type), cp.Name,
		cp.Description, cp.DescriptionAI, string(cp.Trust),
		tagsJSON, ownersJSON, propsJSON,
		cp.CreatedAt, cp.UpdatedAt, cp.CreatedBy, cp.UpdatedBy,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("qualified_name %q: %w", cp.QualifiedName, graph.ErrConflict)
		}
		return nil, fmt.Errorf("insert asset: %w", err)
	}
	return &cp, nil
}

func (s *Store) GetAsset(ctx context.Context, id string) (*graph.Asset, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	row := s.pool.QueryRow(ctx, selectAssetSQL+` WHERE tenant_id = $1 AND id = $2`, tenant, id)
	return scanAsset(row)
}

func (s *Store) GetAssetByQualifiedName(ctx context.Context, qn string) (*graph.Asset, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	row := s.pool.QueryRow(ctx,
		selectAssetSQL+` WHERE tenant_id = $1 AND qualified_name = $2`, tenant, qn)
	return scanAsset(row)
}

func (s *Store) UpdateAsset(ctx context.Context, a *graph.Asset) (*graph.Asset, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if a == nil || a.ID == "" {
		return nil, graph.ErrInvalidArgument
	}
	cp := *a
	cp.TenantID = tenant
	cp.UpdatedAt = s.now().UTC()

	tagsJSON, ownersJSON, propsJSON, err := encodeAssetJSON(&cp)
	if err != nil {
		return nil, err
	}

	const q = `
		UPDATE assets SET
			qualified_name = $3,
			type = $4,
			name = $5,
			description = $6,
			description_ai = $7,
			trust = $8,
			tags = $9,
			owners = $10,
			properties = $11,
			updated_at = $12,
			updated_by = $13
		WHERE tenant_id = $1 AND id = $2
		RETURNING ` + assetColumns
	row := s.pool.QueryRow(ctx, q,
		tenant, cp.ID,
		cp.QualifiedName, string(cp.Type), cp.Name,
		cp.Description, cp.DescriptionAI, string(cp.Trust),
		tagsJSON, ownersJSON, propsJSON,
		cp.UpdatedAt, cp.UpdatedBy,
	)
	updated, err := scanAsset(row)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("qualified_name %q: %w", cp.QualifiedName, graph.ErrConflict)
		}
		return nil, err
	}
	return updated, nil
}

func (s *Store) DeleteAsset(ctx context.Context, id string) error {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM assets WHERE tenant_id = $1 AND id = $2`, tenant, id)
	if err != nil {
		return fmt.Errorf("delete asset: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return graph.ErrNotFound
	}
	return nil
}

func (s *Store) ListAssets(ctx context.Context, opts storage.ListAssetsOptions) ([]*graph.Asset, string, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, "", err
	}
	limit := opts.PageSize
	if limit <= 0 || limit > 500 {
		limit = 50
	}

	args := []any{tenant}
	where := `tenant_id = $1`
	if opts.Type != graph.AssetTypeUnspecified {
		args = append(args, string(opts.Type))
		where += fmt.Sprintf(` AND type = $%d`, len(args))
	}
	if opts.PageToken != "" {
		args = append(args, opts.PageToken)
		where += fmt.Sprintf(` AND qualified_name > (SELECT qualified_name FROM assets WHERE id = $%d AND tenant_id = $1)`, len(args))
	}
	args = append(args, limit+1) // +1 to detect more pages
	q := selectAssetSQL + ` WHERE ` + where + ` ORDER BY qualified_name ASC LIMIT $` + fmt.Sprint(len(args))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list assets: %w", err)
	}
	defer rows.Close()

	var out []*graph.Asset
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			return nil, "", err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	var next string
	if len(out) > limit {
		next = out[limit-1].ID
		out = out[:limit]
	}
	return out, next, nil
}

func (s *Store) CreateEdge(ctx context.Context, e *graph.Edge) (*graph.Edge, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if e == nil || e.SourceID == "" || e.TargetID == "" || e.Kind == "" {
		return nil, graph.ErrInvalidArgument
	}
	cp := *e
	cp.TenantID = tenant
	if cp.ID == "" {
		cp.ID = newUUID()
	}
	cp.CreatedAt = s.now().UTC()

	propsJSON, err := json.Marshal(orEmptyMap(cp.Properties))
	if err != nil {
		return nil, err
	}

	const q = `
		INSERT INTO edges (id, tenant_id, kind, source_id, target_id, properties, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`
	_, err = s.pool.Exec(ctx, q,
		cp.ID, cp.TenantID, string(cp.Kind), cp.SourceID, cp.TargetID,
		propsJSON, cp.CreatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("edge %s→%s: %w", cp.SourceID, cp.TargetID, graph.ErrConflict)
		}
		if isForeignKeyViolation(err) {
			return nil, fmt.Errorf("source or target asset: %w", graph.ErrNotFound)
		}
		return nil, fmt.Errorf("insert edge: %w", err)
	}
	return &cp, nil
}

func (s *Store) DeleteEdge(ctx context.Context, id string) error {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM edges WHERE tenant_id = $1 AND id = $2`, tenant, id)
	if err != nil {
		return fmt.Errorf("delete edge: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return graph.ErrNotFound
	}
	return nil
}

func (s *Store) Neighbors(ctx context.Context, assetID string, opts storage.NeighborsOptions) ([]*graph.Edge, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	limit := opts.Limit
	if limit <= 0 || limit > 5000 {
		limit = 500
	}

	pivot := "source_id"
	if !opts.Outgoing {
		pivot = "target_id"
	}
	args := []any{tenant, assetID}
	where := fmt.Sprintf(`tenant_id = $1 AND %s = $2`, pivot)
	if opts.Kind != "" {
		args = append(args, string(opts.Kind))
		where += fmt.Sprintf(` AND kind = $%d`, len(args))
	}
	args = append(args, limit)

	q := `
		SELECT id, tenant_id, kind, source_id, target_id, properties, created_at
		FROM edges WHERE ` + where + ` ORDER BY created_at LIMIT $` + fmt.Sprint(len(args))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("neighbors: %w", err)
	}
	defer rows.Close()

	var out []*graph.Edge
	for rows.Next() {
		var e graph.Edge
		var kind string
		var props []byte
		if err := rows.Scan(&e.ID, &e.TenantID, &kind, &e.SourceID, &e.TargetID, &props, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.Kind = graph.EdgeKind(kind)
		if len(props) > 0 {
			if err := json.Unmarshal(props, &e.Properties); err != nil {
				return nil, fmt.Errorf("decode properties: %w", err)
			}
		}
		out = append(out, &e)
	}
	return out, rows.Err()
}

// ----- helpers -----

const assetColumns = `id, tenant_id, qualified_name, type, name,
	description, description_ai, trust, tags, owners, properties,
	created_at, updated_at, created_by, updated_by`

const selectAssetSQL = `SELECT ` + assetColumns + ` FROM assets`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanAsset(row rowScanner) (*graph.Asset, error) {
	var a graph.Asset
	var typ, trust string
	var tagsJSON, ownersJSON, propsJSON []byte
	err := row.Scan(
		&a.ID, &a.TenantID, &a.QualifiedName, &typ, &a.Name,
		&a.Description, &a.DescriptionAI, &trust,
		&tagsJSON, &ownersJSON, &propsJSON,
		&a.CreatedAt, &a.UpdatedAt, &a.CreatedBy, &a.UpdatedBy,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, graph.ErrNotFound
		}
		return nil, fmt.Errorf("scan asset: %w", err)
	}
	a.Type = graph.AssetType(typ)
	a.Trust = graph.TrustLevel(trust)
	if err := unmarshalIfPresent(tagsJSON, &a.Tags); err != nil {
		return nil, err
	}
	if err := unmarshalIfPresent(ownersJSON, &a.Owners); err != nil {
		return nil, err
	}
	if err := unmarshalIfPresent(propsJSON, &a.Properties); err != nil {
		return nil, err
	}
	return &a, nil
}

func unmarshalIfPresent(b []byte, dst any) error {
	if len(b) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, dst); err != nil {
		return fmt.Errorf("decode jsonb: %w", err)
	}
	return nil
}

func encodeAssetJSON(a *graph.Asset) (tags, owners, props []byte, err error) {
	tags, err = json.Marshal(orEmptyStringSlice(a.Tags))
	if err != nil {
		return
	}
	owners, err = json.Marshal(orEmptyStringSlice(a.Owners))
	if err != nil {
		return
	}
	props, err = json.Marshal(orEmptyMap(a.Properties))
	return
}

func orEmptyStringSlice(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func orEmptyMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Errorf("postgres: rand failed: %w", err))
	}
	// RFC 4122 v4 layout
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	)
}
