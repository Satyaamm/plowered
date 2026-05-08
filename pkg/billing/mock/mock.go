// Package mock is an in-process billing Provider for tests. It records every
// call so tests can assert against the recorded set.
package mock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"github.com/Satyaamm/plowered/pkg/billing"
)

type Provider struct {
	mu sync.Mutex

	customers     map[string]billing.Customer
	subscriptions map[string]billing.Subscription
	usage         []billing.ReportUsageRequest
}

func New() *Provider {
	return &Provider{
		customers:     make(map[string]billing.Customer),
		subscriptions: make(map[string]billing.Subscription),
	}
}

func (p *Provider) Name() string { return "mock" }

func (p *Provider) CreateCustomer(_ context.Context, req billing.CreateCustomerRequest) (billing.Customer, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, c := range p.customers {
		if c.TenantID == req.TenantID && c.Email == req.Email {
			return c, nil
		}
	}
	id := newID("cus_")
	c := billing.Customer{
		ID:        id,
		TenantID:  req.TenantID,
		Email:     req.Email,
		CreatedAt: time.Now().UTC(),
	}
	p.customers[id] = c
	return c, nil
}

func (p *Provider) AttachPlan(_ context.Context, customerID, planID string) (billing.Subscription, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.customers[customerID]; !ok {
		return billing.Subscription{}, billing.ErrNotFound
	}
	now := time.Now().UTC()
	sub := billing.Subscription{
		CustomerID:         customerID,
		PlanID:             planID,
		Status:             billing.SubActive,
		CurrentPeriodStart: now,
		CurrentPeriodEnd:   now.Add(30 * 24 * time.Hour),
	}
	p.subscriptions[customerID] = sub
	return sub, nil
}

func (p *Provider) GetSubscription(_ context.Context, customerID string) (billing.Subscription, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	sub, ok := p.subscriptions[customerID]
	if !ok {
		return billing.Subscription{}, billing.ErrNotFound
	}
	return sub, nil
}

func (p *Provider) ReportUsage(_ context.Context, req billing.ReportUsageRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, u := range p.usage {
		if u.IdempotencyKey != "" && u.IdempotencyKey == req.IdempotencyKey {
			return nil
		}
	}
	p.usage = append(p.usage, req)
	return nil
}

// Recorded snapshots for tests.

func (p *Provider) Usage() []billing.ReportUsageRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]billing.ReportUsageRequest, len(p.usage))
	copy(out, p.usage)
	return out
}

func newID(prefix string) string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}
