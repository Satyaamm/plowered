package policy_test

import (
	"context"
	"testing"

	"github.com/Satyaamm/plowered/internal/core/auth"
	"github.com/Satyaamm/plowered/internal/core/policy"
)

func ctxAndPrincipal(roles ...string) (context.Context, auth.Principal) {
	p := auth.Principal{ID: "u1", TenantID: "t1", Roles: roles}
	return auth.WithPrincipal(context.Background(), p), p
}

func TestRoleViewerCanRead(t *testing.T) {
	e := policy.NewEngine(nil)
	_, p := ctxAndPrincipal("viewer")
	d := e.Allow(context.Background(), p, policy.VerbRead, policy.Resource{
		Type: "asset", TenantID: "t1",
	})
	if !d.Allow {
		t.Errorf("viewer should read: %s", d.Reason)
	}
}

func TestRoleViewerCannotEdit(t *testing.T) {
	e := policy.NewEngine(nil)
	_, p := ctxAndPrincipal("viewer")
	d := e.Allow(context.Background(), p, policy.VerbEdit, policy.Resource{
		Type: "asset", TenantID: "t1",
	})
	if d.Allow {
		t.Errorf("viewer should NOT edit; got allow")
	}
}

func TestRoleAdminCanDoEverything(t *testing.T) {
	e := policy.NewEngine(nil)
	_, p := ctxAndPrincipal("admin")
	for _, v := range []policy.Verb{
		policy.VerbRead, policy.VerbEdit, policy.VerbPropose,
		policy.VerbCertify, policy.VerbDelete, policy.VerbRun, policy.VerbAdmin,
	} {
		d := e.Allow(context.Background(), p, v, policy.Resource{Type: "asset", TenantID: "t1"})
		if !d.Allow {
			t.Errorf("admin denied %q: %s", v, d.Reason)
		}
	}
}

func TestCrossTenantDenied(t *testing.T) {
	e := policy.NewEngine(nil)
	_, p := ctxAndPrincipal("admin")
	d := e.Allow(context.Background(), p, policy.VerbRead, policy.Resource{
		Type: "asset", TenantID: "OTHER",
	})
	if d.Allow {
		t.Error("cross-tenant should be denied even for admin")
	}
}

func TestRuleAllowsViewerToEditByGroup(t *testing.T) {
	store := policy.NewMemoryRuleStore(policy.Rule{
		ID: "r1", TenantID: "t1", Effect: policy.EffectAllow,
		Verbs: []policy.Verb{policy.VerbEdit},
		Conditions: []policy.Condition{
			{Type: policy.CondPrincipalGroup, Value: "data-team"},
		},
	})
	e := policy.NewEngine(store)
	p := auth.Principal{ID: "u1", TenantID: "t1", Roles: []string{"viewer"}, Groups: []string{"data-team"}}
	d := e.Allow(context.Background(), p, policy.VerbEdit, policy.Resource{Type: "asset", TenantID: "t1"})
	if !d.Allow {
		t.Errorf("viewer-in-data-team should edit via rule: %s", d.Reason)
	}
}

func TestDenyRuleOverridesRoleGrant(t *testing.T) {
	store := policy.NewMemoryRuleStore(policy.Rule{
		ID: "r1", TenantID: "t1", Effect: policy.EffectDeny,
		Verbs: []policy.Verb{policy.VerbDelete},
		Conditions: []policy.Condition{
			{Type: policy.CondResourceTag, Value: "class:pii"},
		},
	})
	e := policy.NewEngine(store)
	_, p := ctxAndPrincipal("admin")
	d := e.Allow(context.Background(), p, policy.VerbDelete, policy.Resource{
		Type: "asset", TenantID: "t1", Tags: []string{"class:pii"},
	})
	if d.Allow {
		t.Errorf("deny rule should override admin role: %s", d.Reason)
	}
}

func TestOwnerCanEditTheirAsset(t *testing.T) {
	store := policy.NewMemoryRuleStore(policy.Rule{
		ID: "r1", Effect: policy.EffectAllow,
		Verbs: []policy.Verb{policy.VerbEdit},
		Conditions: []policy.Condition{
			{Type: policy.CondResourceOwner, Value: "self"},
		},
	})
	e := policy.NewEngine(store)
	p := auth.Principal{ID: "u1", TenantID: "t1", Roles: []string{"viewer"}}
	d := e.Allow(context.Background(), p, policy.VerbEdit, policy.Resource{
		Type: "asset", TenantID: "t1", OwnerIDs: []string{"u1"},
	})
	if !d.Allow {
		t.Errorf("owner should edit own asset: %s", d.Reason)
	}
}
