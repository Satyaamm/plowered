// Package billing is the provider-agnostic abstraction Plowered uses for
// subscription management and metered usage reporting.
//
// Concrete implementations (each a sub-package) plug behind this interface
// without leaking provider-specific types into the rest of the codebase.
// The same router-and-fallback pattern from pkg/llm applies: tenants can
// be routed to different billing providers, and a fallback chain handles
// transient provider outages.
package billing

import (
	"context"
	"errors"
	"time"
)

// Provider is the surface every billing backend implements.
type Provider interface {
	// CreateCustomer registers a new billable customer. Idempotent on
	// (TenantID, Email) — calling twice should return the existing customer.
	CreateCustomer(ctx context.Context, req CreateCustomerRequest) (Customer, error)

	// AttachPlan moves a customer to the named plan. Pro-rates per the
	// provider's policy.
	AttachPlan(ctx context.Context, customerID, planID string) (Subscription, error)

	// GetSubscription returns the current plan + status for a customer.
	GetSubscription(ctx context.Context, customerID string) (Subscription, error)

	// ReportUsage records a metered event (assets crawled, tokens used, etc.)
	// against the customer's current billing period. Implementations must be
	// safe to retry — duplicate events with the same IdempotencyKey are
	// recorded once.
	ReportUsage(ctx context.Context, req ReportUsageRequest) error

	// Name returns a stable identifier for telemetry; never user-facing.
	Name() string
}

type CreateCustomerRequest struct {
	TenantID string
	Email    string
	Name     string
	Country  string // ISO-3166 alpha-2; empty = unknown
	Metadata map[string]string
}

type Customer struct {
	ID         string
	TenantID   string
	Email      string
	CreatedAt  time.Time
}

type Subscription struct {
	CustomerID         string
	PlanID             string
	Status             SubscriptionStatus
	CurrentPeriodStart time.Time
	CurrentPeriodEnd   time.Time
	CancelAt           *time.Time
}

type SubscriptionStatus string

const (
	SubActive   SubscriptionStatus = "active"
	SubTrialing SubscriptionStatus = "trialing"
	SubPastDue  SubscriptionStatus = "past_due"
	SubCanceled SubscriptionStatus = "canceled"
	SubUnknown  SubscriptionStatus = "unknown"
)

type ReportUsageRequest struct {
	CustomerID     string
	MeterID        string  // e.g. "assets_crawled", "llm_tokens"
	Quantity       float64
	OccurredAt     time.Time
	IdempotencyKey string
}

// Sentinel errors. Concrete providers wrap these so callers can use
// errors.Is to branch.
var (
	ErrNotFound         = errors.New("billing: not found")
	ErrAlreadyExists    = errors.New("billing: already exists")
	ErrPlanUnknown      = errors.New("billing: unknown plan")
	ErrRateLimited      = errors.New("billing: rate limited")
	ErrProviderOutage   = errors.New("billing: provider unavailable")
)
