package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/identity"
)

// Postgres-backed implementations of the identity repos. All five share a
// pool and a clock for testability. Schema lives in migrations 0004
// (tenants/users/sessions/memberships) + 0005 (email_verifications).

type IdentityStore struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewIdentityStore(pool *pgxpool.Pool) *IdentityStore {
	return &IdentityStore{pool: pool, now: time.Now}
}

// ----- Tenants -----

func (s *IdentityStore) CreateTenant(ctx context.Context, t *identity.Tenant) (*identity.Tenant, error) {
	cp := *t
	if cp.Tier == "" {
		cp.Tier = "standard"
	}
	if cp.Status == "" {
		cp.Status = "active"
	}
	if cp.Region == "" {
		cp.Region = "us-east-1"
	}
	settings, _ := json.Marshal(cp.Settings)
	if len(settings) == 0 || string(settings) == "null" {
		settings = []byte(`{}`)
	}
	const q = `
		INSERT INTO tenants (slug, name, region, tier, status, settings)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb)
		RETURNING id::text, created_at, updated_at`
	if err := s.pool.QueryRow(ctx, q,
		cp.Slug, cp.Name, cp.Region, cp.Tier, cp.Status, settings,
	).Scan(&cp.ID, &cp.CreatedAt, &cp.UpdatedAt); err != nil {
		if isUniqueViolation(err) {
			return nil, identity.ErrSlugTaken
		}
		return nil, fmt.Errorf("identity: create tenant: %w", err)
	}
	return &cp, nil
}

func (s *IdentityStore) GetTenantBySlug(ctx context.Context, slug string) (*identity.Tenant, error) {
	row := s.pool.QueryRow(ctx, selectTenantSQL+` WHERE slug = $1`, slug)
	return scanTenant(row)
}

func (s *IdentityStore) GetTenantByID(ctx context.Context, id string) (*identity.Tenant, error) {
	row := s.pool.QueryRow(ctx, selectTenantSQL+` WHERE id = $1::uuid`, id)
	return scanTenant(row)
}

const selectTenantSQL = `
	SELECT id::text, slug, name, region, tier, status, settings, created_at, updated_at
	  FROM tenants`

func scanTenant(row rowScanner) (*identity.Tenant, error) {
	var (
		t       identity.Tenant
		settings []byte
	)
	if err := row.Scan(&t.ID, &t.Slug, &t.Name, &t.Region, &t.Tier, &t.Status,
		&settings, &t.CreatedAt, &t.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, identity.ErrNotFound
		}
		return nil, err
	}
	if len(settings) > 0 {
		_ = json.Unmarshal(settings, &t.Settings)
	}
	return &t, nil
}

// ----- UserRepo -----

func (s *IdentityStore) CreateUser(ctx context.Context, u *identity.User) (*identity.User, error) {
	cp := *u
	if cp.Status == "" {
		cp.Status = "active"
	}
	if cp.FullName == "" && (cp.FirstName != "" || cp.LastName != "") {
		cp.FullName = strings.TrimSpace(cp.FirstName + " " + cp.LastName)
	}
	const q = `
		INSERT INTO users (email, full_name, first_name, last_name, phone, phone_country, status, password_hash)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id::text, created_at, updated_at`
	if err := s.pool.QueryRow(ctx, q,
		cp.Email, cp.FullName, cp.FirstName, cp.LastName, cp.Phone, cp.PhoneCountry,
		cp.Status, cp.PasswordHash,
	).Scan(&cp.ID, &cp.CreatedAt, &cp.UpdatedAt); err != nil {
		if isUniqueViolation(err) {
			return nil, identity.ErrEmailTaken
		}
		return nil, fmt.Errorf("identity: create user: %w", err)
	}
	return &cp, nil
}

func (s *IdentityStore) GetByEmail(ctx context.Context, email string) (*identity.User, error) {
	row := s.pool.QueryRow(ctx, selectUserSQL+` WHERE email_lower = lower($1)`, email)
	return scanUser(row)
}

func (s *IdentityStore) GetUserByID(ctx context.Context, id string) (*identity.User, error) {
	row := s.pool.QueryRow(ctx, selectUserSQL+` WHERE id = $1::uuid`, id)
	return scanUser(row)
}

func (s *IdentityStore) MarkEmailVerified(ctx context.Context, id string, at time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE users SET email_verified_at = $2, updated_at = $2
		 WHERE id = $1::uuid AND email_verified_at IS NULL`, id, at)
	if err != nil {
		return fmt.Errorf("identity: mark verified: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Already verified is benign; treat as success.
		return nil
	}
	return nil
}

func (s *IdentityStore) UpdateLastLogin(ctx context.Context, id, ip string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE users
		   SET last_login_at = $2, last_login_ip = $3, updated_at = $2
		 WHERE id = $1::uuid`, id, at, ip)
	if err != nil {
		return fmt.Errorf("identity: update last login: %w", err)
	}
	return nil
}

const selectUserSQL = `
	SELECT id::text, email, full_name,
	       COALESCE(first_name, ''), COALESCE(last_name, ''),
	       COALESCE(phone, ''), COALESCE(phone_country, ''),
	       avatar_url, status, password_hash,
	       mfa_enrolled,
	       COALESCE(last_login_at, '0001-01-01 00:00:00+00'::timestamptz),
	       last_login_ip,
	       COALESCE(locked_at, '0001-01-01 00:00:00+00'::timestamptz),
	       locked_reason,
	       COALESCE(email_verified_at, '0001-01-01 00:00:00+00'::timestamptz),
	       created_at, updated_at
	  FROM users`

func scanUser(row rowScanner) (*identity.User, error) {
	var u identity.User
	if err := row.Scan(
		&u.ID, &u.Email, &u.FullName,
		&u.FirstName, &u.LastName, &u.Phone, &u.PhoneCountry,
		&u.AvatarURL, &u.Status, &u.PasswordHash,
		&u.MFAEnrolled, &u.LastLoginAt, &u.LastLoginIP, &u.LockedAt, &u.LockedReason,
		&u.EmailVerifiedAt, &u.CreatedAt, &u.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, identity.ErrNotFound
		}
		return nil, err
	}
	return &u, nil
}

// ----- MembershipRepo -----

func (s *IdentityStore) CreateMembership(ctx context.Context, m *identity.Membership) error {
	roles, _ := json.Marshal(m.Roles)
	groups, _ := json.Marshal(m.Groups)
	if len(roles) == 0 || string(roles) == "null" {
		roles = []byte(`["viewer"]`)
	}
	if len(groups) == 0 || string(groups) == "null" {
		groups = []byte(`[]`)
	}
	var invitedBy any
	if m.InvitedBy != "" {
		invitedBy = m.InvitedBy
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO tenant_memberships (tenant_id, user_id, roles, groups, invited_by, accepted_at)
		VALUES ($1::uuid, $2::uuid, $3::jsonb, $4::jsonb, $5::uuid, $6)`,
		m.TenantID, m.UserID, roles, groups, invitedBy, m.AcceptedAt,
	)
	if err != nil {
		return fmt.Errorf("identity: create membership: %w", err)
	}
	return nil
}

func (s *IdentityStore) ListForUser(ctx context.Context, userID string) ([]*identity.Membership, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT tenant_id::text, user_id::text, roles, groups,
		       COALESCE(invited_by::text, ''),
		       invited_at,
		       COALESCE(accepted_at, '0001-01-01 00:00:00+00'::timestamptz)
		  FROM tenant_memberships
		 WHERE user_id = $1::uuid`, userID)
	if err != nil {
		return nil, fmt.Errorf("identity: list memberships: %w", err)
	}
	defer rows.Close()
	out := []*identity.Membership{}
	for rows.Next() {
		var m identity.Membership
		var roles, groups []byte
		if err := rows.Scan(&m.TenantID, &m.UserID, &roles, &groups,
			&m.InvitedBy, &m.InvitedAt, &m.AcceptedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(roles, &m.Roles)
		_ = json.Unmarshal(groups, &m.Groups)
		out = append(out, &m)
	}
	return out, rows.Err()
}

func (s *IdentityStore) GetMembership(ctx context.Context, tenantID, userID string) (*identity.Membership, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT tenant_id::text, user_id::text, roles, groups,
		       COALESCE(invited_by::text, ''),
		       invited_at,
		       COALESCE(accepted_at, '0001-01-01 00:00:00+00'::timestamptz)
		  FROM tenant_memberships
		 WHERE tenant_id = $1::uuid AND user_id = $2::uuid`, tenantID, userID)
	var m identity.Membership
	var roles, groups []byte
	if err := row.Scan(&m.TenantID, &m.UserID, &roles, &groups,
		&m.InvitedBy, &m.InvitedAt, &m.AcceptedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, identity.ErrNotFound
		}
		return nil, err
	}
	_ = json.Unmarshal(roles, &m.Roles)
	_ = json.Unmarshal(groups, &m.Groups)
	return &m, nil
}

// ----- SessionRepo -----

func (s *IdentityStore) CreateSession(ctx context.Context, sess *identity.Session) (*identity.Session, error) {
	cp := *sess
	if cp.IssuedAt.IsZero() {
		cp.IssuedAt = s.now().UTC()
	}
	if cp.LastSeenAt.IsZero() {
		cp.LastSeenAt = cp.IssuedAt
	}
	if cp.ExpiresAt.IsZero() {
		cp.ExpiresAt = cp.IssuedAt.Add(identity.SessionTTL)
	}
	const q = `
		INSERT INTO sessions (user_id, tenant_id, ip, user_agent, issued_at, last_seen_at, expires_at)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7)
		RETURNING id::text`
	if err := s.pool.QueryRow(ctx, q,
		cp.UserID, cp.TenantID, cp.IP, cp.UserAgent,
		cp.IssuedAt, cp.LastSeenAt, cp.ExpiresAt,
	).Scan(&cp.ID); err != nil {
		return nil, fmt.Errorf("identity: create session: %w", err)
	}
	return &cp, nil
}

func (s *IdentityStore) GetSession(ctx context.Context, id string) (*identity.Session, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id::text, user_id::text, tenant_id::text, ip, user_agent,
		       issued_at, last_seen_at, expires_at,
		       COALESCE(revoked_at, '0001-01-01 00:00:00+00'::timestamptz),
		       revoked_reason
		  FROM sessions WHERE id = $1::uuid`, id)
	var sess identity.Session
	if err := row.Scan(
		&sess.ID, &sess.UserID, &sess.TenantID, &sess.IP, &sess.UserAgent,
		&sess.IssuedAt, &sess.LastSeenAt, &sess.ExpiresAt,
		&sess.RevokedAt, &sess.RevokedReason,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, identity.ErrNotFound
		}
		return nil, err
	}
	return &sess, nil
}

func (s *IdentityStore) RevokeSession(ctx context.Context, id, reason string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE sessions SET revoked_at = $2, revoked_reason = $3
		 WHERE id = $1::uuid AND revoked_at IS NULL`, id, at, reason)
	if err != nil {
		return fmt.Errorf("identity: revoke session: %w", err)
	}
	return nil
}

func (s *IdentityStore) TouchSession(ctx context.Context, id string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE sessions SET last_seen_at = $2 WHERE id = $1::uuid`, id, at)
	return err
}

func (s *IdentityStore) RevokeAllSessionsForUser(ctx context.Context, userID, reason string, at time.Time) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE sessions SET revoked_at = $2, revoked_reason = $3
		 WHERE user_id = $1::uuid AND revoked_at IS NULL`,
		userID, at, reason)
	if err != nil {
		return fmt.Errorf("identity: revoke all sessions: %w", err)
	}
	return nil
}

func (s *IdentityStore) ListActiveSessionsForUser(ctx context.Context, userID string) ([]*identity.Session, error) {
	const q = `
		SELECT id::text, user_id::text, tenant_id::text,
		       COALESCE(ip, ''), COALESCE(user_agent, ''),
		       issued_at, last_seen_at, expires_at,
		       COALESCE(revoked_at, '0001-01-01 00:00:00+00'::timestamptz),
		       COALESCE(revoked_reason, '')
		  FROM sessions
		 WHERE user_id = $1::uuid
		   AND revoked_at IS NULL
		   AND expires_at > now()
		 ORDER BY last_seen_at DESC`
	rows, err := s.pool.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("identity: list sessions: %w", err)
	}
	defer rows.Close()
	var out []*identity.Session
	for rows.Next() {
		var sess identity.Session
		if err := rows.Scan(
			&sess.ID, &sess.UserID, &sess.TenantID,
			&sess.IP, &sess.UserAgent,
			&sess.IssuedAt, &sess.LastSeenAt, &sess.ExpiresAt,
			&sess.RevokedAt, &sess.RevokedReason,
		); err != nil {
			return nil, err
		}
		out = append(out, &sess)
	}
	return out, rows.Err()
}

func (s *IdentityStore) UpdatePassword(ctx context.Context, userID, newHash string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE users SET password_hash = $2, updated_at = now()
		 WHERE id = $1::uuid`,
		userID, newHash)
	if err != nil {
		return fmt.Errorf("identity: update password: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return identity.ErrNotFound
	}
	return nil
}

func (s *IdentityStore) UpdateProfile(ctx context.Context, userID string, p identity.ProfileUpdate) error {
	// COALESCE-with-NULLIF trick lets us conditionally update each
	// column: pass empty string for "skip"; pass a real value to write.
	// full_name is re-derived server-side when either name changes.
	tag, err := s.pool.Exec(ctx, `
		UPDATE users
		   SET first_name = COALESCE(NULLIF($2, ''), first_name),
		       last_name  = COALESCE(NULLIF($3, ''), last_name),
		       full_name  = CASE
		                      WHEN $2 <> '' OR $3 <> '' THEN
		                        trim(COALESCE(NULLIF($2, ''), first_name) || ' ' || COALESCE(NULLIF($3, ''), last_name))
		                      ELSE full_name
		                    END,
		       phone         = $4,
		       phone_country = $5,
		       updated_at    = now()
		 WHERE id = $1::uuid`,
		userID, p.FirstName, p.LastName, p.Phone, p.PhoneCountry)
	if err != nil {
		return fmt.Errorf("identity: update profile: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return identity.ErrNotFound
	}
	return nil
}

// ----- VerificationRepo -----

func (s *IdentityStore) CreateVerification(ctx context.Context, v *identity.Verification) error {
	cp := *v
	if cp.ExpiresAt.IsZero() {
		cp.ExpiresAt = s.now().UTC().Add(identity.VerificationTTL)
	}
	if cp.Purpose == "" {
		cp.Purpose = identity.PurposeVerifyEmail
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO email_verifications (user_id, token, purpose, expires_at)
		VALUES ($1::uuid, $2, $3, $4)`,
		cp.UserID, cp.Token, string(cp.Purpose), cp.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("identity: create verification: %w", err)
	}
	return nil
}

func (s *IdentityStore) GetByToken(ctx context.Context, token string) (*identity.Verification, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id::text, user_id::text, token, purpose, expires_at,
		       COALESCE(used_at, '0001-01-01 00:00:00+00'::timestamptz),
		       created_at
		  FROM email_verifications WHERE token = $1`, token)
	var v identity.Verification
	var purpose string
	if err := row.Scan(&v.ID, &v.UserID, &v.Token, &purpose, &v.ExpiresAt, &v.UsedAt, &v.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, identity.ErrTokenInvalid
		}
		return nil, err
	}
	v.Purpose = identity.VerificationPurpose(purpose)
	return &v, nil
}

func (s *IdentityStore) MarkUsed(ctx context.Context, id string, at time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE email_verifications SET used_at = $2
		 WHERE id = $1::uuid AND used_at IS NULL`, id, at)
	if err != nil {
		return fmt.Errorf("identity: mark verification used: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return identity.ErrTokenInvalid
	}
	return nil
}

// ----- Login lockout -----

// RecordFailedLogin uses an atomic UPDATE so concurrent failed logins
// (botnet spraying) can't race to bypass the threshold. The reset_at
// window is honored: a stale counter (older than FailedLoginWindow) is
// treated as 0 + restarted.
func (s *IdentityStore) RecordFailedLogin(ctx context.Context, userID string, at time.Time) (int, error) {
	resetCutoff := at.Add(-identity.FailedLoginWindow)
	const q = `
		UPDATE users
		   SET failed_login_count = CASE
		                             WHEN failed_login_reset_at IS NULL OR failed_login_reset_at < $2 THEN 1
		                             ELSE failed_login_count + 1
		                           END,
		       failed_login_reset_at = CASE
		                             WHEN failed_login_reset_at IS NULL OR failed_login_reset_at < $2 THEN $3
		                             ELSE failed_login_reset_at
		                           END
		 WHERE id = $1::uuid
		RETURNING failed_login_count`
	var n int
	if err := s.pool.QueryRow(ctx, q, userID, resetCutoff, at.Add(identity.FailedLoginWindow)).Scan(&n); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, identity.ErrNotFound
		}
		return 0, fmt.Errorf("identity: record failed login: %w", err)
	}
	return n, nil
}

func (s *IdentityStore) ResetFailedLogin(ctx context.Context, userID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE users SET failed_login_count = 0, failed_login_reset_at = NULL
		 WHERE id = $1::uuid`, userID)
	return err
}

func (s *IdentityStore) LockUser(ctx context.Context, userID, reason string, at time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE users SET status = 'locked', locked_at = $2, locked_reason = $3, updated_at = now()
		 WHERE id = $1::uuid`,
		userID, at, reason)
	if err != nil {
		return fmt.Errorf("identity: lock user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return identity.ErrNotFound
	}
	return nil
}

func (s *IdentityStore) PseudonymiseUser(ctx context.Context, userID, stubEmail string, at time.Time) error {
	// Wipe every PII column atomically. The UNIQUE(email_lower)
	// constraint on users is honored by stubbing the email to a
	// deleted-<uuid>@deleted.invalid placeholder generated by the
	// caller, so the user's old email is free for re-registration.
	tag, err := s.pool.Exec(ctx, `
		UPDATE users
		   SET email         = $2,
		       first_name    = '',
		       last_name     = '',
		       full_name     = '',
		       phone         = '',
		       phone_country = '',
		       avatar_url    = '',
		       password_hash = '',
		       status        = 'deleted',
		       locked_at     = NULL,
		       locked_reason = '',
		       updated_at    = $3
		 WHERE id = $1::uuid`,
		userID, stubEmail, at)
	if err != nil {
		return fmt.Errorf("identity: pseudonymise user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return identity.ErrNotFound
	}
	return nil
}

func (s *IdentityStore) UnlockUser(ctx context.Context, userID string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE users
		   SET status = 'active', locked_at = NULL, locked_reason = '',
		       failed_login_count = 0, failed_login_reset_at = NULL,
		       updated_at = now()
		 WHERE id = $1::uuid`,
		userID)
	if err != nil {
		return fmt.Errorf("identity: unlock user: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return identity.ErrNotFound
	}
	return nil
}

// ----- Memberships: team-scoped list / mutate -----

func (s *IdentityStore) ListMembershipsForTenant(ctx context.Context, tenantID string) ([]*identity.MembershipWithUser, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT tm.tenant_id::text, tm.user_id::text, tm.roles, tm.groups,
		       COALESCE(tm.invited_by::text, ''),
		       tm.invited_at,
		       COALESCE(tm.accepted_at, '0001-01-01 00:00:00+00'::timestamptz),
		       u.email,
		       COALESCE(u.first_name, ''),
		       COALESCE(u.last_name,  ''),
		       COALESCE(u.full_name,  ''),
		       u.status
		  FROM tenant_memberships tm
		  JOIN users u ON u.id = tm.user_id
		 WHERE tm.tenant_id = $1::uuid
		 ORDER BY tm.invited_at ASC`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("identity: list members: %w", err)
	}
	defer rows.Close()
	out := []*identity.MembershipWithUser{}
	for rows.Next() {
		var m identity.MembershipWithUser
		var roles, groups []byte
		if err := rows.Scan(
			&m.TenantID, &m.UserID, &roles, &groups,
			&m.InvitedBy, &m.InvitedAt, &m.AcceptedAt,
			&m.Email, &m.FirstName, &m.LastName, &m.FullName, &m.Status,
		); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(roles, &m.Roles)
		_ = json.Unmarshal(groups, &m.Groups)
		out = append(out, &m)
	}
	return out, rows.Err()
}

func (s *IdentityStore) UpdateMembershipRoles(ctx context.Context, tenantID, userID string, roles []string) error {
	if roles == nil {
		roles = []string{}
	}
	raw, _ := json.Marshal(roles)
	tag, err := s.pool.Exec(ctx, `
		UPDATE tenant_memberships SET roles = $3::jsonb
		 WHERE tenant_id = $1::uuid AND user_id = $2::uuid`,
		tenantID, userID, raw)
	if err != nil {
		return fmt.Errorf("identity: update membership: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return identity.ErrNotFound
	}
	return nil
}

func (s *IdentityStore) DeleteMembership(ctx context.Context, tenantID, userID string) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM tenant_memberships
		 WHERE tenant_id = $1::uuid AND user_id = $2::uuid`,
		tenantID, userID)
	if err != nil {
		return fmt.Errorf("identity: delete membership: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return identity.ErrNotFound
	}
	return nil
}

// ----- Invitations -----

func (s *IdentityStore) CreateInvite(ctx context.Context, i *identity.Invite) error {
	if i.Roles == nil {
		i.Roles = []string{"viewer"}
	}
	roles, _ := json.Marshal(i.Roles)
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO invitations (tenant_id, email, roles, token, invited_by, expires_at)
		VALUES ($1, $2, $3::jsonb, $4, $5, $6)
		RETURNING id::text, created_at`,
		i.TenantID, i.Email, roles, i.Token, i.InvitedBy, i.ExpiresAt,
	).Scan(&i.ID, &i.CreatedAt); err != nil {
		return fmt.Errorf("identity: create invite: %w", err)
	}
	return nil
}

func (s *IdentityStore) GetInviteByToken(ctx context.Context, token string) (*identity.Invite, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id::text, tenant_id, email, roles, token, invited_by,
		       expires_at,
		       COALESCE(accepted_at, '0001-01-01 00:00:00+00'::timestamptz),
		       COALESCE(revoked_at,  '0001-01-01 00:00:00+00'::timestamptz),
		       created_at
		  FROM invitations WHERE token = $1`, token)
	return scanInvite(row)
}

func (s *IdentityStore) GetInvite(ctx context.Context, tenantID, id string) (*identity.Invite, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id::text, tenant_id, email, roles, token, invited_by,
		       expires_at,
		       COALESCE(accepted_at, '0001-01-01 00:00:00+00'::timestamptz),
		       COALESCE(revoked_at,  '0001-01-01 00:00:00+00'::timestamptz),
		       created_at
		  FROM invitations
		 WHERE tenant_id = $1 AND id = $2::uuid`, tenantID, id)
	return scanInvite(row)
}

func (s *IdentityStore) ListInvitesForTenant(ctx context.Context, tenantID string, pendingOnly bool) ([]*identity.Invite, error) {
	q := `SELECT id::text, tenant_id, email, roles, token, invited_by,
	            expires_at,
	            COALESCE(accepted_at, '0001-01-01 00:00:00+00'::timestamptz),
	            COALESCE(revoked_at,  '0001-01-01 00:00:00+00'::timestamptz),
	            created_at
	       FROM invitations WHERE tenant_id = $1`
	if pendingOnly {
		q += ` AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at > now()`
	}
	q += ` ORDER BY created_at DESC`
	rows, err := s.pool.Query(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("identity: list invites: %w", err)
	}
	defer rows.Close()
	var out []*identity.Invite
	for rows.Next() {
		inv, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

func (s *IdentityStore) RevokeInvite(ctx context.Context, tenantID, id string, at time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE invitations SET revoked_at = $3
		 WHERE tenant_id = $1 AND id = $2::uuid
		   AND accepted_at IS NULL AND revoked_at IS NULL`,
		tenantID, id, at)
	if err != nil {
		return fmt.Errorf("identity: revoke invite: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return identity.ErrNotFound
	}
	return nil
}

func (s *IdentityStore) MarkInviteAccepted(ctx context.Context, id string, at time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE invitations SET accepted_at = $2
		 WHERE id = $1::uuid AND accepted_at IS NULL AND revoked_at IS NULL`,
		id, at)
	if err != nil {
		return fmt.Errorf("identity: accept invite: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return identity.ErrTokenInvalid
	}
	return nil
}

func scanInvite(row rowScanner) (*identity.Invite, error) {
	var (
		i     identity.Invite
		roles []byte
	)
	if err := row.Scan(
		&i.ID, &i.TenantID, &i.Email, &roles, &i.Token, &i.InvitedBy,
		&i.ExpiresAt, &i.AcceptedAt, &i.RevokedAt, &i.CreatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, identity.ErrNotFound
		}
		return nil, fmt.Errorf("identity: scan invite: %w", err)
	}
	_ = json.Unmarshal(roles, &i.Roles)
	return &i, nil
}

// isUniqueViolation lives in postgres.go; we share the existing helper.

