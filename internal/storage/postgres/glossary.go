package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/glossary"
)

// GlossaryStore is the Postgres-backed glossary.Repo. Terms live in
// public.glossary_terms and assignments in public.term_assignments
// (both tables created by the existing 0004_enterprise migration).
type GlossaryStore struct {
	pool *pgxpool.Pool
}

func NewGlossaryStore(p *pgxpool.Pool) *GlossaryStore { return &GlossaryStore{pool: p} }

func (s *GlossaryStore) List(ctx context.Context, tenantID string) ([]*glossary.Term, error) {
	const q = `
		SELECT id::text, tenant_id::text, name, definition,
		       COALESCE(parent_id::text, ''), status,
		       COALESCE(owner_id::text, ''),
		       created_at, updated_at
		  FROM glossary_terms
		 WHERE tenant_id = $1::uuid
		 ORDER BY name`
	rows, err := s.pool.Query(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list glossary: %w", err)
	}
	defer rows.Close()
	var out []*glossary.Term
	for rows.Next() {
		t, err := scanTerm(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *GlossaryStore) Get(ctx context.Context, tenantID, id string) (*glossary.Term, error) {
	const q = `
		SELECT id::text, tenant_id::text, name, definition,
		       COALESCE(parent_id::text, ''), status,
		       COALESCE(owner_id::text, ''),
		       created_at, updated_at
		  FROM glossary_terms
		 WHERE tenant_id = $1::uuid AND id = $2::uuid`
	t, err := scanTerm(s.pool.QueryRow(ctx, q, tenantID, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, glossary.ErrNotFound
		}
		return nil, fmt.Errorf("get glossary: %w", err)
	}
	return t, nil
}

func (s *GlossaryStore) Create(ctx context.Context, t *glossary.Term) (*glossary.Term, error) {
	if t.Status == "" {
		t.Status = glossary.StatusDraft
	}
	const q = `
		INSERT INTO glossary_terms (tenant_id, name, definition, parent_id, status, owner_id)
		VALUES ($1::uuid, $2, $3, NULLIF($4,'')::uuid, $5, NULLIF($6,'')::uuid)
		RETURNING id::text, created_at, updated_at`
	var id string
	var created, updated = t.CreatedAt, t.UpdatedAt
	if err := s.pool.QueryRow(ctx, q,
		t.TenantID, t.Name, t.Definition, t.ParentID, string(t.Status), t.OwnerID,
	).Scan(&id, &created, &updated); err != nil {
		if isUniqueViolation(err) {
			return nil, glossary.ErrNameTaken
		}
		return nil, fmt.Errorf("create glossary term: %w", err)
	}
	t.ID = id
	t.CreatedAt = created
	t.UpdatedAt = updated
	return t, nil
}

func (s *GlossaryStore) Update(ctx context.Context, t *glossary.Term) (*glossary.Term, error) {
	if t.Status == "" {
		t.Status = glossary.StatusDraft
	}
	const q = `
		UPDATE glossary_terms
		   SET name = $3,
		       definition = $4,
		       parent_id = NULLIF($5,'')::uuid,
		       status = $6,
		       owner_id = NULLIF($7,'')::uuid,
		       updated_at = now()
		 WHERE tenant_id = $1::uuid AND id = $2::uuid
		 RETURNING updated_at`
	var updated = t.UpdatedAt
	if err := s.pool.QueryRow(ctx, q,
		t.TenantID, t.ID, t.Name, t.Definition, t.ParentID, string(t.Status), t.OwnerID,
	).Scan(&updated); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, glossary.ErrNotFound
		}
		if isUniqueViolation(err) {
			return nil, glossary.ErrNameTaken
		}
		return nil, fmt.Errorf("update glossary term: %w", err)
	}
	t.UpdatedAt = updated
	return t, nil
}

func (s *GlossaryStore) Delete(ctx context.Context, tenantID, id string) error {
	const q = `DELETE FROM glossary_terms WHERE tenant_id = $1::uuid AND id = $2::uuid`
	tag, err := s.pool.Exec(ctx, q, tenantID, id)
	if err != nil {
		return fmt.Errorf("delete glossary term: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return glossary.ErrNotFound
	}
	return nil
}

func (s *GlossaryStore) Assign(ctx context.Context, a *glossary.Assignment) error {
	const q = `
		INSERT INTO term_assignments (tenant_id, term_id, asset_id, assigned_by)
		VALUES ($1::uuid, $2::uuid, $3::uuid, NULLIF($4,'')::uuid)
		ON CONFLICT (term_id, asset_id) DO UPDATE
		SET assigned_by = COALESCE(EXCLUDED.assigned_by, term_assignments.assigned_by),
		    assigned_at = now()`
	_, err := s.pool.Exec(ctx, q, a.TenantID, a.TermID, a.AssetID, a.AssignedBy)
	if err != nil {
		return fmt.Errorf("assign term: %w", err)
	}
	return nil
}

func (s *GlossaryStore) Unassign(ctx context.Context, tenantID, termID, assetID string) error {
	const q = `DELETE FROM term_assignments WHERE tenant_id = $1::uuid AND term_id = $2::uuid AND asset_id = $3::uuid`
	tag, err := s.pool.Exec(ctx, q, tenantID, termID, assetID)
	if err != nil {
		return fmt.Errorf("unassign term: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return glossary.ErrNotFound
	}
	return nil
}

func (s *GlossaryStore) AssignmentsByAsset(ctx context.Context, tenantID, assetID string) ([]*glossary.AssignmentView, error) {
	const q = `
		SELECT t.id::text, t.name, t.definition, t.status,
		       a.asset_id::text, a.assigned_at
		  FROM term_assignments a
		  JOIN glossary_terms t ON t.id = a.term_id
		 WHERE a.tenant_id = $1::uuid AND a.asset_id = $2::uuid
		 ORDER BY t.name`
	rows, err := s.pool.Query(ctx, q, tenantID, assetID)
	if err != nil {
		return nil, fmt.Errorf("assignments by asset: %w", err)
	}
	defer rows.Close()
	var out []*glossary.AssignmentView
	for rows.Next() {
		var v glossary.AssignmentView
		var status string
		if err := rows.Scan(&v.TermID, &v.TermName, &v.Definition, &status, &v.AssetID, &v.AssignedAt); err != nil {
			return nil, err
		}
		v.Status = glossary.Status(status)
		out = append(out, &v)
	}
	return out, rows.Err()
}

func (s *GlossaryStore) AssetsByTerm(ctx context.Context, tenantID, termID string) ([]string, error) {
	const q = `SELECT asset_id::text FROM term_assignments WHERE tenant_id = $1::uuid AND term_id = $2::uuid ORDER BY assigned_at DESC`
	rows, err := s.pool.Query(ctx, q, tenantID, termID)
	if err != nil {
		return nil, fmt.Errorf("assets by term: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// scanTerm is a tiny adapter that works with both pgx.Row and pgx.Rows.
func scanTerm(s interface {
	Scan(...any) error
}) (*glossary.Term, error) {
	var t glossary.Term
	var status string
	if err := s.Scan(
		&t.ID, &t.TenantID, &t.Name, &t.Definition,
		&t.ParentID, &status, &t.OwnerID,
		&t.CreatedAt, &t.UpdatedAt,
	); err != nil {
		return nil, err
	}
	t.Status = glossary.Status(status)
	return &t, nil
}

var _ glossary.Repo = (*GlossaryStore)(nil)
