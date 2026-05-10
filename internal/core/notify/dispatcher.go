package notify

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Satyaamm/plowered/internal/core/events"
)

// Dispatcher subscribes to an events.Bus and turns matching events into
// Delivery rows on the configured channels. It owns the channel registry,
// the Store, and the retry policy.
type Dispatcher struct {
	Store    Store
	Logger   *slog.Logger
	Now      func() time.Time

	mu       sync.RWMutex
	channels map[string]Channel // kind → impl
}

func NewDispatcher(store Store) *Dispatcher {
	return &Dispatcher{
		Store:    store,
		Logger:   slog.Default(),
		Now:      time.Now,
		channels: make(map[string]Channel),
	}
}

// Register adds a Channel impl by Kind. Subsequent registrations for the
// same kind replace the previous one.
func (d *Dispatcher) Register(c Channel) {
	d.mu.Lock()
	d.channels[c.Kind()] = c
	d.mu.Unlock()
}

// OnEvent satisfies events.Subscriber. Looks up matching rules and enqueues
// a Delivery for each. Deliveries dispatch synchronously in v0; a follow-up
// moves them onto a worker pool.
func (d *Dispatcher) OnEvent(ctx context.Context, e events.Event) {
	if d.Store == nil {
		return
	}
	rules, err := d.Store.ListRulesForEvent(ctx, e.TenantID, e)
	if err != nil {
		d.logger().Error("notify: list rules", "err", err, "event", e.Type)
		return
	}
	for _, r := range rules {
		if !ruleMatches(r, e) {
			continue
		}
		d.deliverOne(ctx, r, e)
	}
}

func (d *Dispatcher) deliverOne(ctx context.Context, r Rule, e events.Event) {
	channel, err := d.Store.GetChannel(ctx, r.TenantID, r.ChannelID)
	if err != nil {
		d.logger().Error("notify: load channel", "err", err, "channel", r.ChannelID)
		return
	}
	d.mu.RLock()
	impl, ok := d.channels[channel.Kind]
	d.mu.RUnlock()
	if !ok {
		d.logger().Error("notify: no impl for kind", "kind", channel.Kind)
		return
	}

	subject, body := renderTemplate(e)
	delivery := &Delivery{
		ID:             newID(),
		TenantID:       r.TenantID,
		RuleID:         r.ID,
		ChannelID:      r.ChannelID,
		EventID:        e.ID,
		Subject:        subject,
		Body:           body,
		IdempotencyKey: idempotencyKey(r.ID, e),
		Status:         DeliveryQueued,
		CreatedAt:      d.Now().UTC(),
	}
	saved, err := d.Store.CreateDelivery(ctx, delivery)
	if err != nil {
		d.logger().Error("notify: create delivery", "err", err)
		return
	}

	saved.Status = DeliverySending
	saved.Attempts++
	_ = d.Store.UpdateDelivery(ctx, saved)

	if err := impl.Deliver(ctx, *saved); err != nil {
		saved.Status = DeliveryFailed
		saved.LastError = err.Error()
	} else {
		saved.Status = DeliveryDelivered
		saved.DeliveredAt = d.Now().UTC()
	}
	_ = d.Store.UpdateDelivery(ctx, saved)
}

func (d *Dispatcher) logger() *slog.Logger {
	if d.Logger == nil {
		return slog.Default()
	}
	return d.Logger
}

// ruleMatches returns true when the rule's filters all match the event.
func ruleMatches(r Rule, e events.Event) bool {
	if !r.Enabled {
		return false
	}
	if r.TenantID != "" && r.TenantID != e.TenantID {
		return false
	}
	if len(r.EventTypes) > 0 {
		match := false
		for _, t := range r.EventTypes {
			if t == e.Type {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}
	return severityAtLeast(e.Severity, r.MinSeverity)
}

func severityAtLeast(have, threshold events.Severity) bool {
	rank := func(s events.Severity) int {
		switch s {
		case events.SeverityCritical:
			return 4
		case events.SeverityError:
			return 3
		case events.SeverityWarning:
			return 2
		case events.SeverityInfo:
			return 1
		}
		return 1
	}
	if threshold == "" {
		return true
	}
	return rank(have) >= rank(threshold)
}

func renderTemplate(e events.Event) (subject, body string) {
	subject = fmt.Sprintf("[%s] %s on %s", e.Severity, e.Type, e.ResourceType)
	var b strings.Builder
	fmt.Fprintf(&b, "Event: %s\n", e.Type)
	fmt.Fprintf(&b, "Severity: %s\n", e.Severity)
	fmt.Fprintf(&b, "Tenant: %s\n", e.TenantID)
	fmt.Fprintf(&b, "Resource: %s/%s\n", e.ResourceType, e.ResourceID)
	fmt.Fprintf(&b, "OccurredAt: %s\n", e.OccurredAt.UTC().Format(time.RFC3339))
	if len(e.Attributes) > 0 {
		b.WriteString("\nAttributes:\n")
		for k, v := range e.Attributes {
			fmt.Fprintf(&b, "  %s: %v\n", k, v)
		}
	}
	return subject, b.String()
}

// idempotencyKey produces a stable key per (rule, event). Receivers use it
// to drop duplicates if Plowered retries.
func idempotencyKey(ruleID string, e events.Event) string {
	h := sha256.New()
	_, _ = h.Write([]byte(ruleID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(e.ID))
	return hex.EncodeToString(h.Sum(nil))[:24]
}

func newID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "del-fallback"
	}
	return hex.EncodeToString(b[:])
}
