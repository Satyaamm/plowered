// Package policy implements RBAC + light ABAC enforcement. Storage code
// calls Authorizer.Allow before returning a row; handlers cannot bypass it
// because they don't talk to SQL directly (SECURITY.md §3, §4).
//
// Two-tier model:
//
//  1. Workspace roles: viewer | editor | steward | admin (assigned per
//     workspace membership; cached on the Principal).
//  2. Per-resource overrides: tag-based allow / deny rules. The default
//     policy is "deny" until a role grant or an explicit Rule says
//     otherwise.
package policy

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"

	"github.com/Satyaamm/plowered/internal/core/auth"
)

// Verb names a permission. Stable strings; new verbs are additive.
type Verb string

const (
	VerbRead     Verb = "read"
	VerbEdit     Verb = "edit"
	VerbPropose  Verb = "propose"
	VerbCertify  Verb = "certify"
	VerbDelete   Verb = "delete"
	VerbRun      Verb = "run"
	VerbAdmin    Verb = "admin"

	// VerbPurge permanently removes a tombstone from the recycle bin.
	// Only super_admin holds this verb in the default matrix.
	VerbPurge Verb = "purge"
)

// Resource is the thing being acted upon. Plowered uses a small fixed set
// today; new resource types are added as new sub-systems land.
type Resource struct {
	Type     string         // "asset" | "pipeline" | "check" | "notification"
	ID       string
	TenantID string
	Tags     []string       // for tag-based rules
	OwnerIDs []string       // for "owner can edit" patterns
	Extra    map[string]any // resource-specific fields
}

// Decision is the result of an authorization check.
type Decision struct {
	Allow  bool
	Reason string // human-readable, surfaced in audit logs and 403 responses
}

func deny(reason string) Decision { return Decision{Allow: false, Reason: reason} }
func allow(reason string) Decision { return Decision{Allow: true, Reason: reason} }

// Authorizer answers "may principal perform verb on resource?"
type Authorizer interface {
	Allow(ctx context.Context, principal auth.Principal, verb Verb, resource Resource) Decision
}

// Engine is the default Authorizer. It applies built-in role grants then
// consults a Store-backed list of per-resource Rules. Rules with
// Effect=Deny override allows; ties go to deny.
type Engine struct {
	Store RuleStore // optional; nil disables ABAC overrides
}

func NewEngine(store RuleStore) *Engine { return &Engine{Store: store} }

// Allow implements Authorizer.
func (e *Engine) Allow(ctx context.Context, p auth.Principal, v Verb, r Resource) Decision {
	if p.ID == "" {
		return deny("no principal")
	}
	if p.TenantID == "" {
		return deny("no tenant on principal")
	}
	if r.TenantID != "" && r.TenantID != p.TenantID {
		// Should never reach this — storage already filters by tenant_id.
		// Defense in depth.
		return deny("cross-tenant access denied")
	}

	// Role-based grant.
	if roleAllows(p, v, r) {
		// Check for an explicit deny rule overriding the role grant.
		if e.Store != nil {
			rules, err := e.Store.RulesForResource(ctx, r.Type, r.ID, r.TenantID)
			if err == nil {
				if d := evaluateRules(rules, p, v, r); d.Allow == false && d.Reason != "" {
					return d
				}
			}
		}
		return allow(fmt.Sprintf("role grant: %v", p.Roles))
	}

	// No role grant. Look for an explicit allow rule.
	if e.Store != nil {
		rules, err := e.Store.RulesForResource(ctx, r.Type, r.ID, r.TenantID)
		if err == nil {
			if d := evaluateRules(rules, p, v, r); d.Allow {
				return d
			}
		}
	}
	return deny(fmt.Sprintf("no grant for verb %q", v))
}

// roleAllows codifies the default role → verb matrix.
//
//	super_admin → every verb, plus the "purge" verb on tombstones (no
//	            other role can permanently delete from the recycle bin).
//	admin       → every verb except VerbPurge.
//	steward     → read/edit/propose/certify/run.
//	editor      → read/edit/propose/run.
//	viewer      → read only.
func roleAllows(p auth.Principal, v Verb, _ Resource) bool {
	for _, r := range p.Roles {
		switch r {
		case "super_admin":
			return true
		case "admin":
			if v == VerbPurge {
				continue // only super_admin gets purge
			}
			return true
		case "steward":
			switch v {
			case VerbRead, VerbEdit, VerbPropose, VerbCertify, VerbRun:
				return true
			}
		case "editor":
			switch v {
			case VerbRead, VerbEdit, VerbPropose, VerbRun:
				return true
			}
		case "viewer":
			if v == VerbRead {
				return true
			}
		}
	}
	return false
}

// HasRole reports whether the principal carries the given role.
func HasRole(p auth.Principal, role string) bool {
	for _, r := range p.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// Effect is rule polarity.
type Effect string

const (
	EffectAllow Effect = "allow"
	EffectDeny  Effect = "deny"
)

// Rule is a per-resource grant or deny. Conditions ALL must match for the
// rule to apply.
type Rule struct {
	ID         string
	TenantID   string
	Effect     Effect
	Verbs      []Verb
	Conditions []Condition
}

// Condition matches a property of either the principal or the resource.
type Condition struct {
	Type  ConditionType
	Value string
}

type ConditionType string

const (
	CondPrincipalRole  ConditionType = "principal.role"   // value = role name
	CondPrincipalGroup ConditionType = "principal.group"  // value = group id
	CondResourceTag    ConditionType = "resource.tag"     // value = tag id/name
	CondResourceOwner  ConditionType = "resource.owner"   // value = "self" — principal must be in OwnerIDs
)

// RuleStore is the persistence interface. Memory + Postgres impls live in
// internal/storage/<backend>/.
type RuleStore interface {
	RulesForResource(ctx context.Context, resourceType, resourceID, tenantID string) ([]Rule, error)
}

// RuleRepo is the broader CRUD surface the HTTP layer needs.
type RuleRepo interface {
	RuleStore
	AddRule(r Rule) Rule
	DeleteRule(tenantID, id string) bool
	ListRules(tenantID string) []Rule
}

func evaluateRules(rules []Rule, p auth.Principal, v Verb, r Resource) Decision {
	matched := Decision{} // zero = unmatched
	for _, rule := range rules {
		if !verbInList(v, rule.Verbs) {
			continue
		}
		if !conditionsMatch(rule.Conditions, p, r) {
			continue
		}
		// Deny wins over allow.
		if rule.Effect == EffectDeny {
			return deny("rule " + rule.ID)
		}
		matched = allow("rule " + rule.ID)
	}
	return matched
}

func verbInList(v Verb, list []Verb) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func conditionsMatch(conds []Condition, p auth.Principal, r Resource) bool {
	for _, c := range conds {
		if !conditionMatch(c, p, r) {
			return false
		}
	}
	return true
}

func conditionMatch(c Condition, p auth.Principal, r Resource) bool {
	switch c.Type {
	case CondPrincipalRole:
		for _, role := range p.Roles {
			if role == c.Value {
				return true
			}
		}
		return false
	case CondPrincipalGroup:
		for _, g := range p.Groups {
			if g == c.Value {
				return true
			}
		}
		return false
	case CondResourceTag:
		for _, t := range r.Tags {
			if t == c.Value {
				return true
			}
		}
		return false
	case CondResourceOwner:
		if c.Value != "self" {
			return false
		}
		for _, o := range r.OwnerIDs {
			if o == p.ID {
				return true
			}
		}
		return false
	}
	return false
}

// AllowAll is a permissive Authorizer for tests and the embedded dev mode.
// Production wiring uses Engine.
type AllowAll struct{}

func (AllowAll) Allow(_ context.Context, _ auth.Principal, _ Verb, _ Resource) Decision {
	return allow("allow-all")
}

// MemoryRuleStore is an in-process RuleStore for tests + embedded mode.
type MemoryRuleStore struct {
	mu    sync.RWMutex
	rules []Rule
}

func NewMemoryRuleStore(rules ...Rule) *MemoryRuleStore { return &MemoryRuleStore{rules: rules} }

func (m *MemoryRuleStore) RulesForResource(_ context.Context, resourceType, resourceID, tenantID string) ([]Rule, error) {
	if m == nil {
		return nil, errors.New("nil store")
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Rule, 0, len(m.rules))
	for _, r := range m.rules {
		if r.TenantID != "" && r.TenantID != tenantID {
			continue
		}
		out = append(out, r)
	}
	_ = resourceType
	_ = resourceID
	return out, nil
}

// AddRule appends a Rule. ID is assigned if blank.
func (m *MemoryRuleStore) AddRule(r Rule) Rule {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r.ID == "" {
		r.ID = newRuleID()
	}
	m.rules = append(m.rules, r)
	return r
}

// DeleteRule removes a rule by ID, scoped to tenantID. Returns true if a
// rule was removed.
func (m *MemoryRuleStore) DeleteRule(tenantID, id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, r := range m.rules {
		if r.ID == id && (tenantID == "" || r.TenantID == "" || r.TenantID == tenantID) {
			m.rules = append(m.rules[:i], m.rules[i+1:]...)
			return true
		}
	}
	return false
}

// ListRules returns rules for a tenant.
func (m *MemoryRuleStore) ListRules(tenantID string) []Rule {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Rule, 0, len(m.rules))
	for _, r := range m.rules {
		if r.TenantID == "" || r.TenantID == tenantID {
			out = append(out, r)
		}
	}
	return out
}

func newRuleID() string {
	var b [12]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		return "rule-fallback"
	}
	return hex.EncodeToString(b[:])
}
