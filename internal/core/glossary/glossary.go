// Package glossary owns the business-glossary domain — Terms (with an
// optional parent for hierarchy) and Term-to-Asset assignments.
//
// Terms have a status lifecycle (draft / approved / deprecated) and a
// uniqueness constraint per (tenant, name). Assignments link terms to
// catalog assets so the asset detail UI can show "this column means …"
// without the user navigating to a separate glossary view.
package glossary

import (
	"context"
	"errors"
	"time"
)

// Status enumerates the lifecycle states of a Term. Producers default to
// "draft"; data stewards promote to "approved" when the definition is
// blessed; "deprecated" preserves the term for history but removes it
// from picker UIs.
type Status string

const (
	StatusDraft      Status = "draft"
	StatusApproved   Status = "approved"
	StatusDeprecated Status = "deprecated"
)

// Term is one business glossary entry. Hierarchy is captured by ParentID;
// a NULL parent_id is a root term. Owner is the user_id of the steward
// responsible for keeping the definition current.
type Term struct {
	ID         string
	TenantID   string
	Name       string
	Definition string
	ParentID   string
	Status     Status
	OwnerID    string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Assignment is one term ↔ asset link. (TermID, AssetID) is the natural
// primary key. AssignedBy is the user_id of whoever made the link.
type Assignment struct {
	TenantID   string
	TermID     string
	AssetID    string
	AssignedBy string
	AssignedAt time.Time
}

// AssignmentView decorates an Assignment with the term's display fields
// for "what terms are on this asset" queries — saves the UI a follow-up
// fetch per row.
type AssignmentView struct {
	TermID     string
	TermName   string
	Definition string
	Status     Status
	AssetID    string
	AssignedAt time.Time
}

// Repo is the persistence interface. The Postgres implementation lives
// in internal/storage/postgres; an in-memory variant supports tests.
type Repo interface {
	List(ctx context.Context, tenantID string) ([]*Term, error)
	Get(ctx context.Context, tenantID, id string) (*Term, error)
	Create(ctx context.Context, t *Term) (*Term, error)
	Update(ctx context.Context, t *Term) (*Term, error)
	Delete(ctx context.Context, tenantID, id string) error

	Assign(ctx context.Context, a *Assignment) error
	Unassign(ctx context.Context, tenantID, termID, assetID string) error
	AssignmentsByAsset(ctx context.Context, tenantID, assetID string) ([]*AssignmentView, error)
	AssetsByTerm(ctx context.Context, tenantID, termID string) ([]string, error)
}

// ErrNotFound is returned for missing rows.
var ErrNotFound = errors.New("glossary: not found")

// ErrNameTaken is returned when a (tenant, name) collision occurs.
var ErrNameTaken = errors.New("glossary: term name already exists in this workspace")
