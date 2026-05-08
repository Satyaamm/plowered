package mock_test

import (
	"context"
	"testing"
	"time"

	"github.com/Satyaamm/plowered/pkg/billing"
	"github.com/Satyaamm/plowered/pkg/billing/mock"
)

func TestCreateCustomerIsIdempotent(t *testing.T) {
	p := mock.New()
	ctx := context.Background()
	req := billing.CreateCustomerRequest{TenantID: "t1", Email: "a@b.com"}

	c1, err := p.CreateCustomer(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	c2, err := p.CreateCustomer(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if c1.ID != c2.ID {
		t.Errorf("idempotent CreateCustomer should return same ID, got %q vs %q", c1.ID, c2.ID)
	}
}

func TestAttachPlanRequiresCustomer(t *testing.T) {
	p := mock.New()
	if _, err := p.AttachPlan(context.Background(), "missing", "free"); err == nil {
		t.Error("expected ErrNotFound for unknown customer")
	}
}

func TestReportUsageIdempotency(t *testing.T) {
	p := mock.New()
	ctx := context.Background()
	c, _ := p.CreateCustomer(ctx, billing.CreateCustomerRequest{TenantID: "t1", Email: "a@b.com"})
	req := billing.ReportUsageRequest{
		CustomerID:     c.ID,
		MeterID:        "assets_crawled",
		Quantity:       100,
		OccurredAt:     time.Now(),
		IdempotencyKey: "evt-1",
	}
	for i := 0; i < 3; i++ {
		if err := p.ReportUsage(ctx, req); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(p.Usage()); got != 1 {
		t.Errorf("usage entries = %d, want 1 (idempotent)", got)
	}
}
