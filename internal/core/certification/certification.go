// Package certification implements the asset certification workflow:
// any user can propose certification for an asset; stewards / admins
// review the proposal and approve or reject; certified assets can
// later be revoked. Every transition is recorded in append-only
// history so an auditor can answer "when, why, by whom".
//
// Design choices:
//
//   - Status lives on a separate row, not on assets, so reviewers can
//     run a single-index query for "what's waiting on me" without
//     scanning the catalog.
//   - The current state of an asset is the most-recent row by
//     proposed_at. We never UPDATE rows in place — Approve/Reject
//     produce a new row carrying the proposal's id forward in
//     review_note metadata. This makes the table append-only at the
//     application level and keeps the audit story trivial.
//   - The Service does not enforce role gating itself. Role checks
//     live at the HTTP layer where the authenticated principal is
//     available. The Service trusts its caller — same posture as
//     every other core/* service.
package certification

import (
	"context"
	"errors"
	"time"
)

// Status names a transition in the certification workflow.
type Status string

const (
	// StatusProposed is the initial state when any user proposes an
	// asset for certification. Waiting on a reviewer.
	StatusProposed Status = "proposed"
	// StatusCertified means a reviewer has approved the proposal. The
	// asset shows the certified badge.
	StatusCertified Status = "certified"
	// StatusRejected means a reviewer declined the proposal. The asset
	// reverts to its prior state; the rejection is retained for audit.
	StatusRejected Status = "rejected"
	// StatusRevoked means a previously-certified asset has been
	// un-certified (e.g. owner change, quality degraded).
	StatusRevoked Status = "revoked"
)

// Certification is one row in the asset_certifications history table.
type Certification struct {
	ID            string     `json:"id"`
	TenantID      string     `json:"tenant_id"`
	AssetID       string     `json:"asset_id"`
	Status        Status     `json:"status"`
	ProposedBy    string     `json:"proposed_by,omitempty"`
	ProposedAt    time.Time  `json:"proposed_at"`
	ReviewedBy    string     `json:"reviewed_by,omitempty"`
	ReviewedAt    *time.Time `json:"reviewed_at,omitempty"`
	Justification string     `json:"justification,omitempty"`
	ReviewNote    string     `json:"review_note,omitempty"`
}

// Store is the persistence surface the Service depends on.
type Store interface {
	// Create inserts a new row. The store generates ID + ProposedAt
	// when blank.
	Create(ctx context.Context, c *Certification) (*Certification, error)
	// Get returns ErrNotFound when the id doesn't exist in this tenant.
	Get(ctx context.Context, tenantID, id string) (*Certification, error)
	// Latest returns the most-recent row for an asset, or nil if none.
	Latest(ctx context.Context, tenantID, assetID string) (*Certification, error)
	// History returns every row for an asset, newest first.
	History(ctx context.Context, tenantID, assetID string) ([]*Certification, error)
	// ListPending returns only assets whose LATEST row is status=proposed.
	// Older proposals that have since been approved/rejected are excluded.
	// limit caps the response (0 ⇒ store-default).
	ListPending(ctx context.Context, tenantID string, limit int) ([]*Certification, error)
}

// ErrNotFound is returned when a row id doesn't match.
var ErrNotFound = errors.New("certification: not found")

// Service is the application surface the HTTP layer holds.
type Service struct {
	Store Store
}

// Propose creates a new "proposed" row for the asset. The caller can
// re-propose after rejection; we don't dedupe pending proposals on
// the assumption that a stale proposal is rare and an audit-visible
// duplicate is preferable to silently dropping the second click.
func (s *Service) Propose(ctx context.Context, tenantID, assetID, proposedBy, justification string) (*Certification, error) {
	if tenantID == "" || assetID == "" {
		return nil, errors.New("certification: tenant + asset required")
	}
	return s.Store.Create(ctx, &Certification{
		TenantID:      tenantID,
		AssetID:       assetID,
		Status:        StatusProposed,
		ProposedBy:    proposedBy,
		Justification: justification,
	})
}

// Approve transitions an asset's current proposal into certified.
// The proposalID must be the LATEST row for its asset — older
// proposals that have already been resolved are rejected as stale.
func (s *Service) Approve(ctx context.Context, tenantID, proposalID, reviewerID, note string) (*Certification, error) {
	prop, err := s.requireActiveProposal(ctx, tenantID, proposalID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return s.Store.Create(ctx, &Certification{
		TenantID:      tenantID,
		AssetID:       prop.AssetID,
		Status:        StatusCertified,
		ProposedBy:    prop.ProposedBy,
		Justification: prop.Justification,
		ReviewedBy:    reviewerID,
		ReviewedAt:    &now,
		ReviewNote:    note,
	})
}

// Reject closes the asset's current proposal without certifying. Same
// "latest-row" guard as Approve so a stale proposal can't be acted on.
func (s *Service) Reject(ctx context.Context, tenantID, proposalID, reviewerID, note string) (*Certification, error) {
	prop, err := s.requireActiveProposal(ctx, tenantID, proposalID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return s.Store.Create(ctx, &Certification{
		TenantID:      tenantID,
		AssetID:       prop.AssetID,
		Status:        StatusRejected,
		ProposedBy:    prop.ProposedBy,
		Justification: prop.Justification,
		ReviewedBy:    reviewerID,
		ReviewedAt:    &now,
		ReviewNote:    note,
	})
}

// requireActiveProposal loads the proposal by ID and confirms it is
// still the asset's latest certification row. Prevents double-resolve
// against an already-handled proposal.
func (s *Service) requireActiveProposal(ctx context.Context, tenantID, proposalID string) (*Certification, error) {
	prop, err := s.Store.Get(ctx, tenantID, proposalID)
	if err != nil {
		return nil, err
	}
	if prop.Status != StatusProposed {
		return nil, errors.New("certification: only proposed certifications can be reviewed")
	}
	latest, err := s.Store.Latest(ctx, tenantID, prop.AssetID)
	if err != nil {
		return nil, err
	}
	if latest == nil || latest.ID != prop.ID {
		return nil, errors.New("certification: proposal has been superseded by a newer one")
	}
	return prop, nil
}

// Revoke un-certifies an asset. Requires the latest row to be
// "certified"; the revoked row records the reviewer who pulled the
// plug + their reason.
func (s *Service) Revoke(ctx context.Context, tenantID, assetID, reviewerID, note string) (*Certification, error) {
	latest, err := s.Store.Latest(ctx, tenantID, assetID)
	if err != nil {
		return nil, err
	}
	if latest == nil || latest.Status != StatusCertified {
		return nil, errors.New("certification: asset is not currently certified")
	}
	now := time.Now().UTC()
	return s.Store.Create(ctx, &Certification{
		TenantID:      tenantID,
		AssetID:       assetID,
		Status:        StatusRevoked,
		ProposedBy:    latest.ProposedBy,
		Justification: latest.Justification,
		ReviewedBy:    reviewerID,
		ReviewedAt:    &now,
		ReviewNote:    note,
	})
}

// Latest is a pass-through to the store.
func (s *Service) Latest(ctx context.Context, tenantID, assetID string) (*Certification, error) {
	return s.Store.Latest(ctx, tenantID, assetID)
}

// History returns every row for an asset, newest first.
func (s *Service) History(ctx context.Context, tenantID, assetID string) ([]*Certification, error) {
	return s.Store.History(ctx, tenantID, assetID)
}

// Pending returns the review queue — every asset whose latest row is
// still proposed (not yet approved/rejected).
func (s *Service) Pending(ctx context.Context, tenantID string, limit int) ([]*Certification, error) {
	return s.Store.ListPending(ctx, tenantID, limit)
}
