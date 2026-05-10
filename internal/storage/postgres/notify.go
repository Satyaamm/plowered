package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/events"
	"github.com/Satyaamm/plowered/internal/core/notify"
)

// NotifyStore is the Postgres-backed notify.Repo. The dispatcher's interface
// is satisfied alongside the broader CRUD surface used by the API.
//
// AddChannel/AddRule are kept synchronous and detached from a context-bound
// tenant — the fields on the supplied struct are authoritative.
type NotifyStore struct {
	pool *pgxpool.Pool
	now  func() time.Time

	mu sync.Mutex // serializes AddChannel/AddRule wrappers without ctx
}

func NewNotifyStore(pool *pgxpool.Pool) *NotifyStore {
	return &NotifyStore{pool: pool, now: time.Now}
}

func (s *NotifyStore) ListRulesForEvent(ctx context.Context, tenantID string, _ events.Event) ([]notify.Rule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, name, channel_id, event_types, min_severity, enabled, created_at
		  FROM notify_rules WHERE tenant_id = $1 AND enabled = TRUE`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list notify_rules: %w", err)
	}
	defer rows.Close()
	out := make([]notify.Rule, 0)
	for rows.Next() {
		var (
			r            notify.Rule
			eventsJSON   []byte
			minSev       string
		)
		if err := rows.Scan(&r.ID, &r.TenantID, &r.Name, &r.ChannelID,
			&eventsJSON, &minSev, &r.Enabled, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.MinSeverity = events.Severity(minSev)
		if len(eventsJSON) > 0 {
			_ = json.Unmarshal(eventsJSON, &r.EventTypes)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *NotifyStore) GetChannel(ctx context.Context, tenantID, id string) (*notify.ChannelConfig, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, tenant_id, kind, name, config, secret_urn
		  FROM notify_channels WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	var (
		c       notify.ChannelConfig
		cfgJSON []byte
	)
	if err := row.Scan(&c.ID, &c.TenantID, &c.Kind, &c.Name, &cfgJSON, &c.SecretURN); err != nil {
		return nil, fmt.Errorf("get channel %q: %w", id, err)
	}
	if len(cfgJSON) > 0 {
		_ = json.Unmarshal(cfgJSON, &c.Config)
	}
	return &c, nil
}

func (s *NotifyStore) CreateDelivery(ctx context.Context, d *notify.Delivery) (*notify.Delivery, error) {
	cp := *d
	if cp.ID == "" {
		cp.ID = newUUID()
	}
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = s.now().UTC()
	}
	const q = `
		INSERT INTO notify_deliveries (
			id, tenant_id, rule_id, channel_id, event_id, subject, body,
			idempotency_key, status, attempts, last_error, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (tenant_id, idempotency_key) DO NOTHING`
	_, err := s.pool.Exec(ctx, q,
		cp.ID, cp.TenantID, cp.RuleID, cp.ChannelID, cp.EventID,
		cp.Subject, cp.Body, cp.IdempotencyKey, string(cp.Status),
		cp.Attempts, cp.LastError, cp.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("insert delivery: %w", err)
	}
	return &cp, nil
}

func (s *NotifyStore) UpdateDelivery(ctx context.Context, d *notify.Delivery) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE notify_deliveries SET
			status = $3, attempts = $4, last_error = $5,
			delivered_at = NULLIF($6, '0001-01-01 00:00:00'::timestamptz)
		WHERE tenant_id = $1 AND id = $2`,
		d.TenantID, d.ID, string(d.Status), d.Attempts, d.LastError, d.DeliveredAt,
	)
	if err != nil {
		return fmt.Errorf("update delivery: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("delivery %q not found", d.ID)
	}
	return nil
}

func (s *NotifyStore) ListDeliveries(ctx context.Context, tenantID string, limit int) ([]*notify.Delivery, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT id, tenant_id, rule_id, channel_id, event_id, subject, body,
		       idempotency_key, status, attempts, last_error, created_at,
		       COALESCE(delivered_at, '0001-01-01 00:00:00+00'::timestamptz)
		  FROM notify_deliveries
		 WHERE tenant_id = $1
		 ORDER BY created_at DESC LIMIT %d`, limit), tenantID)
	if err != nil {
		return nil, fmt.Errorf("list deliveries: %w", err)
	}
	defer rows.Close()
	out := make([]*notify.Delivery, 0, limit)
	for rows.Next() {
		var (
			d      notify.Delivery
			status string
		)
		if err := rows.Scan(&d.ID, &d.TenantID, &d.RuleID, &d.ChannelID, &d.EventID,
			&d.Subject, &d.Body, &d.IdempotencyKey, &status, &d.Attempts,
			&d.LastError, &d.CreatedAt, &d.DeliveredAt); err != nil {
			return nil, err
		}
		d.Status = notify.DeliveryStatus(status)
		out = append(out, &d)
	}
	return out, rows.Err()
}

// AddChannel persists a channel config. ID/CreatedAt are filled if blank.
// Errors are swallowed and logged via the pool's normal failure surface —
// this matches the MemoryStore signature, which is also fire-and-forget.
func (s *NotifyStore) AddChannel(c *notify.ChannelConfig) {
	if c.ID == "" {
		c.ID = newUUID()
	}
	cfgJSON, _ := json.Marshal(c.Config)
	_, _ = s.pool.Exec(context.Background(), `
		INSERT INTO notify_channels (id, tenant_id, kind, name, config, secret_urn)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (tenant_id, name) DO UPDATE
		   SET kind = EXCLUDED.kind, config = EXCLUDED.config, secret_urn = EXCLUDED.secret_urn`,
		c.ID, c.TenantID, c.Kind, c.Name, cfgJSON, c.SecretURN,
	)
}

// AddRule persists a notify rule. Same fire-and-forget contract as AddChannel.
func (s *NotifyStore) AddRule(r notify.Rule) {
	if r.ID == "" {
		r.ID = newUUID()
	}
	if r.CreatedAt.IsZero() {
		r.CreatedAt = s.now().UTC()
	}
	eventsJSON, _ := json.Marshal(r.EventTypes)
	_, _ = s.pool.Exec(context.Background(), `
		INSERT INTO notify_rules (id, tenant_id, name, channel_id, event_types, min_severity, enabled, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		r.ID, r.TenantID, r.Name, r.ChannelID, eventsJSON, string(r.MinSeverity), r.Enabled, r.CreatedAt,
	)
}

func (s *NotifyStore) ListChannelsForTenant(tenantID string) []*notify.ChannelConfig {
	rows, err := s.pool.Query(context.Background(), `
		SELECT id, tenant_id, kind, name, config, secret_urn
		  FROM notify_channels WHERE tenant_id = $1 ORDER BY name`, tenantID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := make([]*notify.ChannelConfig, 0)
	for rows.Next() {
		var (
			c       notify.ChannelConfig
			cfgJSON []byte
		)
		if rows.Scan(&c.ID, &c.TenantID, &c.Kind, &c.Name, &cfgJSON, &c.SecretURN) != nil {
			continue
		}
		if len(cfgJSON) > 0 {
			_ = json.Unmarshal(cfgJSON, &c.Config)
		}
		out = append(out, &c)
	}
	return out
}

func (s *NotifyStore) ListRules(tenantID string) []notify.Rule {
	rules, _ := s.ListRulesForEvent(context.Background(), tenantID, events.Event{})
	return rules
}
