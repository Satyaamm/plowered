// Package notify is the notification framework: configurable Channels,
// matching Rules, idempotent Deliveries, and an event-bus subscriber that
// routes events.Event to the right channel.
package notify

import (
	"context"
	"errors"
	"time"

	"github.com/Satyaamm/plowered/internal/core/events"
)

// Channel delivers notifications. Concrete impls: log (built-in),
// webhook (built-in), and any custom sub-package.
type Channel interface {
	// Kind identifies the channel type ("log", "webhook", "email", …).
	Kind() string
	// Deliver attempts to send one notification. Implementations MUST be safe
	// to retry — duplicate calls with the same Delivery.ID should be no-ops.
	Deliver(ctx context.Context, d Delivery) error
}

// Rule decides which events flow to which channels.
type Rule struct {
	ID         string
	TenantID   string
	Name       string
	ChannelID  string
	EventTypes []events.Type      // empty = match all
	MinSeverity events.Severity
	Enabled    bool
	CreatedAt  time.Time
}

// ChannelConfig is the persisted shape of a channel instance. Concrete
// secrets live in a vault, referenced by URN.
type ChannelConfig struct {
	ID       string
	TenantID string
	Kind     string
	Name     string
	Config   map[string]any // type-specific (URLs, headers, etc.)
	SecretURN string        // optional vault reference for auth tokens
}

// Delivery is one attempt to send one notification. Persisted so retries
// can resume and the audit trail is complete.
type Delivery struct {
	ID             string
	TenantID       string
	RuleID         string
	ChannelID      string
	EventID        string
	Subject        string
	Body           string
	IdempotencyKey string // de-dupe at the receiver
	Status         DeliveryStatus
	Attempts       int
	LastError      string
	CreatedAt      time.Time
	DeliveredAt    time.Time
}

type DeliveryStatus string

const (
	DeliveryQueued    DeliveryStatus = "queued"
	DeliverySending   DeliveryStatus = "sending"
	DeliveryDelivered DeliveryStatus = "delivered"
	DeliveryFailed    DeliveryStatus = "failed"
)

// Store persists rules, channels, and deliveries.
type Store interface {
	ListRulesForEvent(ctx context.Context, tenantID string, e events.Event) ([]Rule, error)
	GetChannel(ctx context.Context, tenantID, channelID string) (*ChannelConfig, error)
	CreateDelivery(ctx context.Context, d *Delivery) (*Delivery, error)
	UpdateDelivery(ctx context.Context, d *Delivery) error
	ListDeliveries(ctx context.Context, tenantID string, limit int) ([]*Delivery, error)
}

// Sentinel errors.
var (
	ErrUnknownChannel = errors.New("notify: unknown channel kind")
	ErrTransient      = errors.New("notify: transient error, retry safe")
)
