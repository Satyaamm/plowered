package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/Satyaamm/plowered/internal/core/events"
)

// LogChannel writes notifications to slog. Useful for development and as a
// fallback when no other channel is configured.
type LogChannel struct {
	Logger *slog.Logger
}

func (c *LogChannel) Kind() string { return "log" }

func (c *LogChannel) Deliver(ctx context.Context, d Delivery) error {
	logger := c.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.InfoContext(ctx, "notify",
		"delivery_id", d.ID,
		"rule_id", d.RuleID,
		"subject", d.Subject,
		"body", d.Body,
		"idempotency_key", d.IdempotencyKey,
	)
	return nil
}

// WebhookChannel POSTs the rendered body to the configured URL. The HTTP
// client honors a per-call timeout and counts non-2xx responses as failures
// — the dispatcher's retry layer decides whether to try again.
type WebhookChannel struct {
	HTTPClient *http.Client
	// URLForChannel resolves a ChannelConfig to a destination URL. Callers
	// inject this so the channel doesn't need to know about the Store.
	URLForChannel func(channelID string) (string, map[string]string, error)
}

func NewWebhookChannel() *WebhookChannel {
	return &WebhookChannel{
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}
}

func (*WebhookChannel) Kind() string { return "webhook" }

// webhookPayload is the canonical JSON shape posted to user webhooks.
type webhookPayload struct {
	DeliveryID     string `json:"delivery_id"`
	IdempotencyKey string `json:"idempotency_key"`
	EventID        string `json:"event_id"`
	Subject        string `json:"subject"`
	Body           string `json:"body"`
	Timestamp      string `json:"timestamp"`
}

func (c *WebhookChannel) Deliver(ctx context.Context, d Delivery) error {
	if c.URLForChannel == nil {
		return fmt.Errorf("webhook: URLForChannel not configured")
	}
	url, headers, err := c.URLForChannel(d.ChannelID)
	if err != nil {
		return fmt.Errorf("webhook: resolve url: %w", err)
	}
	body, _ := json.Marshal(webhookPayload{
		DeliveryID:     d.ID,
		IdempotencyKey: d.IdempotencyKey,
		EventID:        d.EventID,
		Subject:        d.Subject,
		Body:           d.Body,
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", d.IdempotencyKey)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook: %w (transient)", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		return fmt.Errorf("webhook: client error %d", resp.StatusCode)
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("webhook: server error %d (transient)", resp.StatusCode)
	}
	return nil
}

// Repo is the broader surface the HTTP layer needs: list/create channels +
// rules in addition to the dispatcher's Store.
type Repo interface {
	Store
	AddChannel(c *ChannelConfig)
	AddRule(r Rule)
	ListChannelsForTenant(tenantID string) []*ChannelConfig
	ListRules(tenantID string) []Rule
}

// MemoryStore is an in-process Store for tests and the embedded dev mode.
type MemoryStore struct {
	mu         sync.Mutex
	rules      []Rule
	channels   map[string]*ChannelConfig
	deliveries []*Delivery
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{channels: make(map[string]*ChannelConfig)}
}

func (m *MemoryStore) AddRule(r Rule)             { m.mu.Lock(); m.rules = append(m.rules, r); m.mu.Unlock() }
func (m *MemoryStore) AddChannel(c *ChannelConfig) {
	m.mu.Lock()
	m.channels[c.ID] = c
	m.mu.Unlock()
}

// ListRules returns rules visible to a tenant (rules with empty TenantID
// are global and surface to every tenant — useful for embedded mode).
func (m *MemoryStore) ListRules(tenantID string) []Rule {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Rule, 0, len(m.rules))
	for _, r := range m.rules {
		if r.TenantID == "" || r.TenantID == tenantID {
			out = append(out, r)
		}
	}
	return out
}

// ListChannelsForTenant returns channels owned by tenantID.
func (m *MemoryStore) ListChannelsForTenant(tenantID string) []*ChannelConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*ChannelConfig, 0, len(m.channels))
	for _, c := range m.channels {
		if c.TenantID == "" || c.TenantID == tenantID {
			cp := *c
			out = append(out, &cp)
		}
	}
	return out
}

func (m *MemoryStore) ListRulesForEvent(_ context.Context, tenantID string, _ events.Event) ([]Rule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Rule, 0, len(m.rules))
	for _, r := range m.rules {
		if r.TenantID == "" || r.TenantID == tenantID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *MemoryStore) GetChannel(_ context.Context, _, channelID string) (*ChannelConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.channels[channelID]
	if !ok {
		return nil, fmt.Errorf("channel %q not found", channelID)
	}
	return c, nil
}

func (m *MemoryStore) CreateDelivery(_ context.Context, d *Delivery) (*Delivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *d
	m.deliveries = append(m.deliveries, &cp)
	return &cp, nil
}

func (m *MemoryStore) UpdateDelivery(_ context.Context, d *Delivery) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, x := range m.deliveries {
		if x.ID == d.ID {
			m.deliveries[i] = d
			return nil
		}
	}
	return fmt.Errorf("delivery %q not found", d.ID)
}

func (m *MemoryStore) ListDeliveries(_ context.Context, tenantID string, limit int) ([]*Delivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Delivery, 0, len(m.deliveries))
	for _, d := range m.deliveries {
		if d.TenantID == tenantID {
			out = append(out, d)
		}
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}
