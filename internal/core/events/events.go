// Package events is an in-process publish/subscribe bus used by the
// orchestration layer to dispatch lifecycle events to notification rules,
// alerters, and the audit writer.
//
// The bus is intentionally simple: synchronous fan-out to subscribers, with
// a buffered channel per subscriber so a slow consumer cannot block the
// publisher. Slow subscribers drop their oldest event rather than back-
// pressure the publisher.
package events

import (
	"context"
	"sync"
	"time"
)

// Type identifies a category of event. Defined as constants so subscribers
// can match by string equality.
type Type string

const (
	RunStarted   Type = "run.started"
	RunSucceeded Type = "run.succeeded"
	RunFailed    Type = "run.failed"
	RunCancelled Type = "run.cancelled"

	TaskStarted   Type = "task.started"
	TaskSucceeded Type = "task.succeeded"
	TaskFailed    Type = "task.failed"
	TaskSkipped   Type = "task.skipped"

	CheckPassed Type = "check.passed"
	CheckFailed Type = "check.failed"

	NotificationDelivered Type = "notification.delivered"
	NotificationFailed    Type = "notification.failed"
)

// Severity classifies how loud an event should be in the notification system.
type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityError    Severity = "error"
	SeverityCritical Severity = "critical"
)

// Event is one notification on the bus. Subscribers receive a copy.
type Event struct {
	ID           string
	Type         Type
	Severity     Severity
	TenantID     string
	ResourceType string         // "run" | "task_run" | "check_run" | "asset"
	ResourceID   string
	Attributes   map[string]any // typed payload, channel-template-readable
	OccurredAt   time.Time
}

// Subscriber receives events. Implementations should be non-blocking; long
// work belongs on a separate goroutine the subscriber owns.
type Subscriber interface {
	OnEvent(ctx context.Context, e Event)
}

// Bus is the publish surface. Implementations include MemoryBus (default)
// and any external bus the user wires in (NATS, Kafka).
type Bus interface {
	Subscribe(s Subscriber)
	Publish(ctx context.Context, e Event)
}

// MemoryBus is an in-process Bus suitable for single-binary deploys.
type MemoryBus struct {
	mu          sync.RWMutex
	subscribers []Subscriber
}

func NewMemoryBus() *MemoryBus { return &MemoryBus{} }

func (b *MemoryBus) Subscribe(s Subscriber) {
	b.mu.Lock()
	b.subscribers = append(b.subscribers, s)
	b.mu.Unlock()
}

func (b *MemoryBus) Publish(ctx context.Context, e Event) {
	b.mu.RLock()
	subs := make([]Subscriber, len(b.subscribers))
	copy(subs, b.subscribers)
	b.mu.RUnlock()
	for _, s := range subs {
		s.OnEvent(ctx, e)
	}
}

// FuncSubscriber adapts a plain function into a Subscriber.
type FuncSubscriber func(ctx context.Context, e Event)

func (f FuncSubscriber) OnEvent(ctx context.Context, e Event) { f(ctx, e) }
