package graph

import (
	"context"
	"fmt"

	"github.com/Satyaamm/plowered/internal/core/auth"
)

// Service is the graph engine that handlers and connectors talk to. It applies
// validation, authorization, and audit hooks before delegating to the Store.
//
// The Service is the place — and the only place — where business rules live.
// Storage knows about rows; handlers know about wire formats; the Service
// knows about meaning.
type Service struct {
	store      Store
	authorizer Authorizer
	audit      AuditSink
}

type Store interface {
	CreateAsset(ctx context.Context, a *Asset) (*Asset, error)
	GetAsset(ctx context.Context, id string) (*Asset, error)
	GetAssetByQualifiedName(ctx context.Context, qn string) (*Asset, error)
	UpdateAsset(ctx context.Context, a *Asset) (*Asset, error)
	DeleteAsset(ctx context.Context, id string) error
}

type Authorizer interface {
	Allow(ctx context.Context, principal auth.Principal, verb string, asset *Asset) error
}

type AuditSink interface {
	Emit(ctx context.Context, event AuditEvent)
}

type AuditEvent struct {
	Action       string
	ResourceType string
	ResourceID   string
	Before       any
	After        any
}

func NewService(store Store, az Authorizer, audit AuditSink) *Service {
	return &Service{store: store, authorizer: az, audit: audit}
}

func (s *Service) CreateAsset(ctx context.Context, a *Asset) (*Asset, error) {
	if err := ValidateAsset(a); err != nil {
		return nil, err
	}
	p, err := auth.PrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.authorizer.Allow(ctx, p, "edit", a); err != nil {
		return nil, fmt.Errorf("create asset: %w", err)
	}
	a.CreatedBy = p.ID
	a.UpdatedBy = p.ID
	created, err := s.store.CreateAsset(ctx, a)
	if err != nil {
		return nil, err
	}
	s.audit.Emit(ctx, AuditEvent{
		Action:       "asset.create",
		ResourceType: "asset",
		ResourceID:   created.ID,
		After:        created,
	})
	return created, nil
}

func (s *Service) GetAsset(ctx context.Context, id string) (*Asset, error) {
	a, err := s.store.GetAsset(ctx, id)
	if err != nil {
		return nil, err
	}
	p, err := auth.PrincipalFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.authorizer.Allow(ctx, p, "read", a); err != nil {
		return nil, err
	}
	return a, nil
}
