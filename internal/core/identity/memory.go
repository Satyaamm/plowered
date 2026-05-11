package identity

import (
	"context"
	"strings"
	"sync"
	"time"
)

// MemoryRepo satisfies Repo entirely in-process. Real signup is a
// non-trivial flow and we want to exercise it in tests without spinning
// up Postgres.
type MemoryRepo struct {
	mu             sync.Mutex
	nextID         int
	tenants        map[string]*Tenant
	tenantsBySlug  map[string]*Tenant
	users          map[string]*User
	usersByEmail   map[string]*User
	memberships    []*Membership
	sessions       map[string]*Session
	verifications  map[string]*Verification
	verifsByToken  map[string]*Verification
	invites        map[string]*Invite
	invitesByToken map[string]*Invite
	failedLogins   map[string]*failCounter
}

func NewMemoryRepo() *MemoryRepo {
	return &MemoryRepo{
		tenants:        map[string]*Tenant{},
		tenantsBySlug:  map[string]*Tenant{},
		users:          map[string]*User{},
		usersByEmail:   map[string]*User{},
		sessions:       map[string]*Session{},
		verifications:  map[string]*Verification{},
		verifsByToken:  map[string]*Verification{},
		invites:        map[string]*Invite{},
		invitesByToken: map[string]*Invite{},
		failedLogins:   map[string]*failCounter{},
	}
}

func (m *MemoryRepo) id() string {
	m.nextID++
	return "mem-" + time.Now().UTC().Format("150405.000000") + "-" + itoa(m.nextID)
}

func itoa(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	out := []byte{}
	for i > 0 {
		out = append([]byte{digits[i%10]}, out...)
		i /= 10
	}
	return string(out)
}

// ----- tenants -----

func (m *MemoryRepo) CreateTenant(_ context.Context, t *Tenant) (*Tenant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tenantsBySlug[t.Slug]; ok {
		return nil, ErrSlugTaken
	}
	cp := *t
	cp.ID = m.id()
	cp.CreatedAt = time.Now().UTC()
	cp.UpdatedAt = cp.CreatedAt
	m.tenants[cp.ID] = &cp
	m.tenantsBySlug[cp.Slug] = &cp
	return &cp, nil
}

func (m *MemoryRepo) GetTenantBySlug(_ context.Context, slug string) (*Tenant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tenantsBySlug[slug]; ok {
		cp := *t
		return &cp, nil
	}
	return nil, ErrNotFound
}

func (m *MemoryRepo) GetTenantByID(_ context.Context, id string) (*Tenant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tenants[id]; ok {
		cp := *t
		return &cp, nil
	}
	return nil, ErrNotFound
}

// ----- users -----

func (m *MemoryRepo) CreateUser(_ context.Context, u *User) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := strings.ToLower(u.Email)
	if _, ok := m.usersByEmail[key]; ok {
		return nil, ErrEmailTaken
	}
	cp := *u
	cp.ID = m.id()
	cp.CreatedAt = time.Now().UTC()
	cp.UpdatedAt = cp.CreatedAt
	if cp.Status == "" {
		cp.Status = "active"
	}
	m.users[cp.ID] = &cp
	m.usersByEmail[key] = &cp
	return &cp, nil
}

func (m *MemoryRepo) GetByEmail(_ context.Context, email string) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.usersByEmail[strings.ToLower(email)]; ok {
		cp := *u
		return &cp, nil
	}
	return nil, ErrNotFound
}

func (m *MemoryRepo) GetUserByID(_ context.Context, id string) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.users[id]; ok {
		cp := *u
		return &cp, nil
	}
	return nil, ErrNotFound
}

func (m *MemoryRepo) MarkEmailVerified(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return ErrNotFound
	}
	if u.EmailVerifiedAt.IsZero() {
		u.EmailVerifiedAt = at
		u.UpdatedAt = at
	}
	return nil
}

func (m *MemoryRepo) UpdateLastLogin(_ context.Context, id, ip string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if u, ok := m.users[id]; ok {
		u.LastLoginAt = at
		u.LastLoginIP = ip
		u.UpdatedAt = at
	}
	return nil
}

// ----- memberships -----

func (m *MemoryRepo) CreateMembership(_ context.Context, mb *Membership) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *mb
	if cp.AcceptedAt.IsZero() {
		cp.AcceptedAt = time.Now().UTC()
	}
	if cp.InvitedAt.IsZero() {
		cp.InvitedAt = cp.AcceptedAt
	}
	m.memberships = append(m.memberships, &cp)
	return nil
}

func (m *MemoryRepo) ListForUser(_ context.Context, userID string) ([]*Membership, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []*Membership{}
	for _, mb := range m.memberships {
		if mb.UserID == userID {
			cp := *mb
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *MemoryRepo) GetMembership(_ context.Context, tenantID, userID string) (*Membership, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, mb := range m.memberships {
		if mb.TenantID == tenantID && mb.UserID == userID {
			cp := *mb
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

// ----- sessions -----

func (m *MemoryRepo) CreateSession(_ context.Context, s *Session) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *s
	cp.ID = m.id()
	if cp.IssuedAt.IsZero() {
		cp.IssuedAt = time.Now().UTC()
	}
	if cp.LastSeenAt.IsZero() {
		cp.LastSeenAt = cp.IssuedAt
	}
	if cp.ExpiresAt.IsZero() {
		cp.ExpiresAt = cp.IssuedAt.Add(SessionTTL)
	}
	m.sessions[cp.ID] = &cp
	return &cp, nil
}

func (m *MemoryRepo) GetSession(_ context.Context, id string) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		cp := *s
		return &cp, nil
	}
	return nil, ErrNotFound
}

func (m *MemoryRepo) RevokeSession(_ context.Context, id, reason string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		if s.RevokedAt.IsZero() {
			s.RevokedAt = at
			s.RevokedReason = reason
		}
		return nil
	}
	return ErrNotFound
}

func (m *MemoryRepo) TouchSession(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		s.LastSeenAt = at
	}
	return nil
}

func (m *MemoryRepo) RevokeAllSessionsForUser(_ context.Context, userID, reason string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		if s.UserID == userID && s.RevokedAt.IsZero() {
			s.RevokedAt = at
			s.RevokedReason = reason
		}
	}
	return nil
}

func (m *MemoryRepo) ListActiveSessionsForUser(_ context.Context, userID string) ([]*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	out := []*Session{}
	for _, s := range m.sessions {
		if s.UserID == userID && s.Active(now) {
			cp := *s
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *MemoryRepo) UpdatePassword(_ context.Context, userID, newHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[userID]
	if !ok {
		return ErrNotFound
	}
	u.PasswordHash = newHash
	u.UpdatedAt = time.Now().UTC()
	return nil
}

// failedLogins stores the in-process equivalent of users.failed_login_*.
// Keyed by user ID. Memory mode only — the Postgres impl persists on
// the users row.
type failCounter struct {
	count   int
	resetAt time.Time
}

func (m *MemoryRepo) RecordFailedLogin(_ context.Context, userID string, at time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failedLogins == nil {
		m.failedLogins = map[string]*failCounter{}
	}
	c, ok := m.failedLogins[userID]
	if !ok || at.After(c.resetAt) {
		c = &failCounter{count: 0, resetAt: at.Add(FailedLoginWindow)}
		m.failedLogins[userID] = c
	}
	c.count++
	return c.count, nil
}

func (m *MemoryRepo) ResetFailedLogin(_ context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.failedLogins, userID)
	return nil
}

func (m *MemoryRepo) LockUser(_ context.Context, userID, reason string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[userID]
	if !ok {
		return ErrNotFound
	}
	u.LockedAt = at
	u.LockedReason = reason
	u.Status = "locked"
	return nil
}

func (m *MemoryRepo) UnlockUser(_ context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[userID]
	if !ok {
		return ErrNotFound
	}
	u.LockedAt = time.Time{}
	u.LockedReason = ""
	u.Status = "active"
	delete(m.failedLogins, userID)
	return nil
}

func (m *MemoryRepo) PseudonymiseUser(_ context.Context, userID, stubEmail string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[userID]
	if !ok {
		return ErrNotFound
	}
	// Free up the email so it can be re-registered, but keep the row.
	delete(m.usersByEmail, u.Email)
	u.Email = stubEmail
	u.FirstName = ""
	u.LastName = ""
	u.FullName = ""
	u.Phone = ""
	u.PhoneCountry = ""
	u.AvatarURL = ""
	u.PasswordHash = ""
	u.Status = "deleted"
	u.UpdatedAt = at
	m.usersByEmail[stubEmail] = u
	return nil
}

func (m *MemoryRepo) UpdateProfile(_ context.Context, userID string, p ProfileUpdate) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[userID]
	if !ok {
		return ErrNotFound
	}
	if p.FirstName != "" {
		u.FirstName = p.FirstName
	}
	if p.LastName != "" {
		u.LastName = p.LastName
	}
	if p.FirstName != "" || p.LastName != "" {
		u.FullName = strings.TrimSpace(u.FirstName + " " + u.LastName)
	}
	// Empty phone is a valid "clear it" instruction; we accept and store
	// the empty string so the user can remove their number entirely.
	u.Phone = p.Phone
	u.PhoneCountry = p.PhoneCountry
	u.UpdatedAt = time.Now().UTC()
	return nil
}

// ----- verifications -----

func (m *MemoryRepo) CreateVerification(_ context.Context, v *Verification) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *v
	cp.ID = m.id()
	if cp.ExpiresAt.IsZero() {
		cp.ExpiresAt = time.Now().UTC().Add(VerificationTTL)
	}
	if cp.Purpose == "" {
		cp.Purpose = PurposeVerifyEmail
	}
	cp.CreatedAt = time.Now().UTC()
	m.verifications[cp.ID] = &cp
	m.verifsByToken[cp.Token] = &cp
	return nil
}

func (m *MemoryRepo) GetByToken(_ context.Context, token string) (*Verification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.verifsByToken[token]; ok {
		cp := *v
		return &cp, nil
	}
	return nil, ErrTokenInvalid
}

func (m *MemoryRepo) MarkUsed(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.verifications[id]
	if !ok {
		return ErrTokenInvalid
	}
	if !v.UsedAt.IsZero() {
		return ErrTokenInvalid
	}
	v.UsedAt = at
	return nil
}

// ----- Memberships: team-scoped -----

func (m *MemoryRepo) ListMembershipsForTenant(_ context.Context, tenantID string) ([]*MembershipWithUser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []*MembershipWithUser{}
	for _, mb := range m.memberships {
		if mb.TenantID != tenantID {
			continue
		}
		u := m.users[mb.UserID]
		mu := &MembershipWithUser{Membership: *mb}
		if u != nil {
			mu.Email = u.Email
			mu.FirstName = u.FirstName
			mu.LastName = u.LastName
			mu.FullName = u.FullName
			mu.Status = u.Status
		}
		out = append(out, mu)
	}
	return out, nil
}

func (m *MemoryRepo) UpdateMembershipRoles(_ context.Context, tenantID, userID string, roles []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, mb := range m.memberships {
		if mb.TenantID == tenantID && mb.UserID == userID {
			mb.Roles = append([]string{}, roles...)
			return nil
		}
	}
	return ErrNotFound
}

func (m *MemoryRepo) DeleteMembership(_ context.Context, tenantID, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, mb := range m.memberships {
		if mb.TenantID == tenantID && mb.UserID == userID {
			m.memberships = append(m.memberships[:i], m.memberships[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

// ----- Invitations -----

func (m *MemoryRepo) CreateInvite(_ context.Context, i *Invite) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i.ID == "" {
		i.ID = m.id()
	}
	if i.CreatedAt.IsZero() {
		i.CreatedAt = time.Now().UTC()
	}
	if i.Roles == nil {
		i.Roles = []string{"viewer"}
	}
	cp := *i
	m.invites[cp.ID] = &cp
	m.invitesByToken[cp.Token] = &cp
	return nil
}

func (m *MemoryRepo) GetInviteByToken(_ context.Context, token string) (*Invite, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.invitesByToken[token]
	if !ok {
		return nil, ErrNotFound
	}
	cp := *v
	return &cp, nil
}

func (m *MemoryRepo) GetInvite(_ context.Context, tenantID, id string) (*Invite, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.invites[id]
	if !ok || v.TenantID != tenantID {
		return nil, ErrNotFound
	}
	cp := *v
	return &cp, nil
}

func (m *MemoryRepo) ListInvitesForTenant(_ context.Context, tenantID string, pendingOnly bool) ([]*Invite, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []*Invite{}
	now := time.Now().UTC()
	for _, v := range m.invites {
		if v.TenantID != tenantID {
			continue
		}
		if pendingOnly && !v.IsPending(now) {
			continue
		}
		cp := *v
		out = append(out, &cp)
	}
	return out, nil
}

func (m *MemoryRepo) RevokeInvite(_ context.Context, tenantID, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.invites[id]
	if !ok || v.TenantID != tenantID {
		return ErrNotFound
	}
	if !v.AcceptedAt.IsZero() || !v.RevokedAt.IsZero() {
		return ErrNotFound
	}
	v.RevokedAt = at
	return nil
}

func (m *MemoryRepo) MarkInviteAccepted(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.invites[id]
	if !ok {
		return ErrTokenInvalid
	}
	if !v.AcceptedAt.IsZero() || !v.RevokedAt.IsZero() {
		return ErrTokenInvalid
	}
	v.AcceptedAt = at
	return nil
}
