// Package identity is the source of truth for "who are you, and which
// tenant?" — it owns the User, Tenant, Membership, Session, and email
// verification types plus the repos that persist them.
//
// Auth flow at a glance:
//
//   1.  POST /v1/auth/signup → creates a Tenant + User (email_verified_at
//       NULL) + a verify_email Verification token. Sends the token over
//       Resend. Returns 202 — the user cannot log in yet.
//   2.  GET  /v1/auth/verify?token=… → flips email_verified_at and marks
//       the verification used. Returns 200 + a redirect-friendly body.
//   3.  POST /v1/auth/login → checks password + email_verified_at IS NOT
//       NULL, mints a Session row, returns it as an HttpOnly cookie.
//   4.  POST /v1/auth/logout → revokes the session.
//   5.  GET  /v1/auth/me → returns the principal for the active session.
//
// SECURITY-§7 entries cover the cookie hardening, session-rotation, and
// rate-limit specifics; this file keeps to the domain types.
package identity

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors. Handlers map these to HTTP status codes; never log
// the literal error message to clients (defense against user-enumeration).
var (
	ErrNotFound          = errors.New("identity: not found")
	ErrEmailTaken        = errors.New("identity: email already registered")
	ErrSlugTaken         = errors.New("identity: workspace slug already taken")
	ErrInvalidPassword   = errors.New("identity: invalid email or password")
	ErrEmailNotVerified  = errors.New("identity: email not verified")
	ErrSessionExpired    = errors.New("identity: session expired")
	ErrTokenInvalid      = errors.New("identity: token invalid or expired")
)

// Tenant is one workspace — every domain row carries `tenant_id`. A
// Tenant is created by signup; further users join via tenant_memberships.
type Tenant struct {
	ID        string    // UUID
	Slug      string    // url-safe; UNIQUE
	Name      string
	Region    string
	Tier      string    // free | standard | enterprise | hipaa
	Status    string    // active | suspended | terminated
	Settings  map[string]any
	CreatedAt time.Time
	UpdatedAt time.Time
}

// User is the person-level identity. password_hash is empty when the
// account is OIDC-only (future-proof). EmailVerifiedAt is the gate the
// login handler checks — until it's set, the user cannot authenticate.
type User struct {
	ID              string
	Email           string
	FirstName       string
	LastName        string
	FullName        string // computed = FirstName + " " + LastName when both set
	Phone           string // subscriber digits, no dial code
	PhoneCountry    string // dial code, e.g. "+1" or "+91"
	AvatarURL       string
	Status          string // active | locked | deleted
	PasswordHash    string
	MFAEnrolled     bool
	LastLoginAt     time.Time
	LastLoginIP     string
	LockedAt        time.Time
	LockedReason    string
	EmailVerifiedAt time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// IsEmailVerified reports whether the user has clicked the verification
// link.
func (u User) IsEmailVerified() bool { return !u.EmailVerifiedAt.IsZero() }

// Membership ties a User to a Tenant with a role set. Multi-tenant users
// have multiple rows; the active session pins one.
type Membership struct {
	TenantID   string
	UserID     string
	Roles      []string
	Groups     []string
	InvitedBy  string
	InvitedAt  time.Time
	AcceptedAt time.Time
}

// MembershipWithUser pairs a Membership with the User columns the team
// page needs. Done at the repo so the HTTP layer issues one query
// instead of N+1.
type MembershipWithUser struct {
	Membership
	Email     string
	FirstName string
	LastName  string
	FullName  string
	Status    string // user.status — "active" / "locked" / etc.
}

// Invite is one outstanding teammate invitation. Token is the secret
// emailed to the invitee; it's stored plaintext because invites are
// single-use and short-lived (7 days). RevokedAt + AcceptedAt are
// mutually exclusive terminal states.
type Invite struct {
	ID         string
	TenantID   string
	Email      string
	Roles      []string
	Token      string
	InvitedBy  string
	ExpiresAt  time.Time
	AcceptedAt time.Time
	RevokedAt  time.Time
	CreatedAt  time.Time
}

// IsPending reports whether the invite still acceptable.
func (i Invite) IsPending(now time.Time) bool {
	if !i.AcceptedAt.IsZero() || !i.RevokedAt.IsZero() {
		return false
	}
	return now.Before(i.ExpiresAt)
}

// InviteTTL is how long an emailed invite link stays valid.
const InviteTTL = 7 * 24 * time.Hour

// Session is one active login. The session.id is the cookie value; it is
// regenerated on login (never reused). Revoked sessions cannot be
// re-validated even before expires_at.
type Session struct {
	ID            string
	UserID        string
	TenantID      string
	IP            string
	UserAgent     string
	IssuedAt      time.Time
	LastSeenAt    time.Time
	ExpiresAt     time.Time
	RevokedAt     time.Time
	RevokedReason string
}

// Active reports whether this session is still usable.
func (s Session) Active(now time.Time) bool {
	if !s.RevokedAt.IsZero() {
		return false
	}
	return now.Before(s.ExpiresAt)
}

// VerificationPurpose enumerates what a token unlocks.
type VerificationPurpose string

const (
	PurposeVerifyEmail   VerificationPurpose = "verify_email"
	PurposePasswordReset VerificationPurpose = "password_reset"
)

// Verification is a single-use token. The token field is the raw value
// emailed to the user; we store it plaintext because it is short-lived
// (24h) and single-use.
type Verification struct {
	ID        string
	UserID    string
	Token     string
	Purpose   VerificationPurpose
	ExpiresAt time.Time
	UsedAt    time.Time
	CreatedAt time.Time
}

// SessionTTL is how long a freshly minted session stays valid by default.
// Idle-revocation lives at a different layer.
const SessionTTL = 14 * 24 * time.Hour

// VerificationTTL is the link lifetime stamped on every fresh token.
const VerificationTTL = 24 * time.Hour

// Repo is the union of every persistence concern in this package. We
// could split it into per-aggregate interfaces but the call sites all
// reach for several at once during a signup or login, so consolidating
// keeps the wiring readable. Method names are unique across aggregates
// so a single struct can satisfy them all without renaming clashes.
type Repo interface {
	// Tenants
	CreateTenant(ctx context.Context, t *Tenant) (*Tenant, error)
	GetTenantBySlug(ctx context.Context, slug string) (*Tenant, error)
	GetTenantByID(ctx context.Context, id string) (*Tenant, error)

	// Users
	CreateUser(ctx context.Context, u *User) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	GetUserByID(ctx context.Context, id string) (*User, error)
	MarkEmailVerified(ctx context.Context, id string, at time.Time) error
	UpdateLastLogin(ctx context.Context, id, ip string, at time.Time) error

	// Memberships
	CreateMembership(ctx context.Context, m *Membership) error
	ListForUser(ctx context.Context, userID string) ([]*Membership, error)
	GetMembership(ctx context.Context, tenantID, userID string) (*Membership, error)
	// ListMembershipsForTenant returns every user that belongs to the
	// tenant, joined with the columns the team page renders.
	ListMembershipsForTenant(ctx context.Context, tenantID string) ([]*MembershipWithUser, error)
	UpdateMembershipRoles(ctx context.Context, tenantID, userID string, roles []string) error
	DeleteMembership(ctx context.Context, tenantID, userID string) error

	// Invitations
	CreateInvite(ctx context.Context, i *Invite) error
	GetInviteByToken(ctx context.Context, token string) (*Invite, error)
	GetInvite(ctx context.Context, tenantID, id string) (*Invite, error)
	ListInvitesForTenant(ctx context.Context, tenantID string, pendingOnly bool) ([]*Invite, error)
	RevokeInvite(ctx context.Context, tenantID, id string, at time.Time) error
	MarkInviteAccepted(ctx context.Context, id string, at time.Time) error

	// Sessions
	CreateSession(ctx context.Context, s *Session) (*Session, error)
	GetSession(ctx context.Context, id string) (*Session, error)
	RevokeSession(ctx context.Context, id, reason string, at time.Time) error
	TouchSession(ctx context.Context, id string, at time.Time) error

	// Verification tokens
	CreateVerification(ctx context.Context, v *Verification) error
	GetByToken(ctx context.Context, token string) (*Verification, error)
	MarkUsed(ctx context.Context, id string, at time.Time) error
}
