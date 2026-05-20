package certification

import (
	"context"
	"strings"
	"sync"
	"testing"
)

type memStore struct {
	mu   sync.Mutex
	rows []*Certification
	seq  int
}

func (m *memStore) Create(_ context.Context, c *Certification) (*Certification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	cp := *c
	cp.ID = "id-" + itoa(m.seq)
	m.rows = append(m.rows, &cp)
	return &cp, nil
}

func (m *memStore) Get(_ context.Context, tenant, id string) (*Certification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, r := range m.rows {
		if r.TenantID == tenant && r.ID == id {
			cp := *r
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

func (m *memStore) Latest(_ context.Context, tenant, assetID string) (*Certification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.rows) - 1; i >= 0; i-- {
		r := m.rows[i]
		if r.TenantID == tenant && r.AssetID == assetID {
			cp := *r
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *memStore) History(_ context.Context, tenant, assetID string) ([]*Certification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []*Certification{}
	for i := len(m.rows) - 1; i >= 0; i-- {
		r := m.rows[i]
		if r.TenantID == tenant && r.AssetID == assetID {
			cp := *r
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (m *memStore) ListPending(_ context.Context, tenant string, limit int) ([]*Certification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Build latest-per-asset by walking newest-first and remembering
	// the first row we see per asset.
	seen := map[string]bool{}
	out := []*Certification{}
	for i := len(m.rows) - 1; i >= 0; i-- {
		r := m.rows[i]
		if r.TenantID != tenant || seen[r.AssetID] {
			continue
		}
		seen[r.AssetID] = true
		if r.Status == StatusProposed {
			cp := *r
			out = append(out, &cp)
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func TestProposeApproveFlow(t *testing.T) {
	svc := &Service{Store: &memStore{}}
	prop, err := svc.Propose(context.Background(), "t1", "a1", "u1", "owner-approved")
	if err != nil {
		t.Fatalf("propose: %v", err)
	}
	if prop.Status != StatusProposed {
		t.Errorf("status: %s", prop.Status)
	}
	approved, err := svc.Approve(context.Background(), "t1", prop.ID, "u-steward", "looks good")
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if approved.Status != StatusCertified {
		t.Errorf("status: %s", approved.Status)
	}
	if approved.ReviewedBy != "u-steward" || approved.ReviewNote != "looks good" {
		t.Errorf("reviewer not captured: %+v", approved)
	}
	if approved.Justification != "owner-approved" {
		t.Errorf("justification not carried forward: %q", approved.Justification)
	}

	latest, _ := svc.Latest(context.Background(), "t1", "a1")
	if latest.Status != StatusCertified {
		t.Errorf("latest after approve: %s", latest.Status)
	}
}

func TestApproveBlocksDoubleResolve(t *testing.T) {
	svc := &Service{Store: &memStore{}}
	prop, _ := svc.Propose(context.Background(), "t1", "a1", "u1", "")
	if _, err := svc.Approve(context.Background(), "t1", prop.ID, "u-steward", ""); err != nil {
		t.Fatalf("first approve: %v", err)
	}
	// The proposal has been superseded by a "certified" row; trying
	// to act on the original proposal id again must fail.
	if _, err := svc.Reject(context.Background(), "t1", prop.ID, "u-steward", ""); err == nil {
		t.Error("expected error rejecting an already-resolved proposal")
	}
	if _, err := svc.Approve(context.Background(), "t1", prop.ID, "u-steward", ""); err == nil {
		t.Error("expected error re-approving an already-resolved proposal")
	}
}

func TestRevokeRequiresCertified(t *testing.T) {
	svc := &Service{Store: &memStore{}}
	if _, err := svc.Revoke(context.Background(), "t1", "missing", "u-steward", ""); err == nil {
		t.Error("expected error revoking a never-certified asset")
	}
	prop, _ := svc.Propose(context.Background(), "t1", "a1", "u1", "")
	_, _ = svc.Approve(context.Background(), "t1", prop.ID, "u-steward", "")
	rev, err := svc.Revoke(context.Background(), "t1", "a1", "u-admin", "quality regressed")
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if rev.Status != StatusRevoked || rev.ReviewedBy != "u-admin" {
		t.Errorf("revoke row: %+v", rev)
	}
}

func TestPendingReturnsOnlyProposed(t *testing.T) {
	svc := &Service{Store: &memStore{}}
	pA, _ := svc.Propose(context.Background(), "t1", "a1", "u1", "")
	_, _ = svc.Propose(context.Background(), "t1", "a2", "u1", "")
	_, _ = svc.Approve(context.Background(), "t1", pA.ID, "u-steward", "")
	pending, err := svc.Pending(context.Background(), "t1", 0)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 1 || pending[0].AssetID != "a2" {
		var assets []string
		for _, p := range pending {
			assets = append(assets, p.AssetID)
		}
		t.Errorf("pending should be [a2] got %v", strings.Join(assets, ","))
	}
}
