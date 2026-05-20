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

func (c *LogChannel) Deliver(ctx context.Context, _ *ChannelConfig, d Delivery) error {
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

// WebhookChannel POSTs the rendered body to the URL stored in
// ChannelConfig.Config["url"]. Optional Config["headers"] (a
// map[string]any of header name → value) is applied per-request. The
// HTTP client honors a per-call timeout and counts non-2xx responses as
// failures — the dispatcher's retry layer decides whether to try again.
type WebhookChannel struct {
	HTTPClient *http.Client
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

func (c *WebhookChannel) Deliver(ctx context.Context, cfg *ChannelConfig, d Delivery) error {
	if cfg == nil {
		return fmt.Errorf("webhook: nil channel config")
	}
	url, _ := cfg.Config["url"].(string)
	if url == "" {
		return fmt.Errorf("webhook: channel %q missing config.url", cfg.ID)
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
	if hdrs, ok := cfg.Config["headers"].(map[string]any); ok {
		for k, v := range hdrs {
			req.Header.Set(k, fmt.Sprint(v))
		}
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
	// LastDeliveryPerRule returns a map of rule_id → most-recent
	// delivered_at timestamp. Rules that have never delivered are
	// absent from the map. Used by the rules-list UI to surface
	// "last fired" inline.
	LastDeliveryPerRule(tenantID string) map[string]time.Time
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

// LastDeliveryPerRule scans the in-memory delivery log for the most
// recent delivered_at per rule. Matches the Postgres semantics.
func (m *MemoryStore) LastDeliveryPerRule(tenantID string) map[string]time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]time.Time{}
	for _, d := range m.deliveries {
		if d.TenantID != tenantID || d.Status != DeliveryDelivered {
			continue
		}
		if existing, ok := out[d.RuleID]; !ok || d.DeliveredAt.After(existing) {
			out[d.RuleID] = d.DeliveredAt
		}
	}
	return out
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
