package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/audit"
)

// AuditStore is the Postgres-backed audit.Writer + audit.Reader. The
// schema lives in migration 0001 (audit_events) extended by 0003
// (hash chain + outcome + http context).
//
// AuditStore maintains a per-tenant chain tail in process memory so each
// row links to the previous one's hash. On warm boot the tail is empty,
// so the first row's PrevHash is nil — verification logic accepts that.
// For multi-replica deploys the recommended pattern is a ChainSyncer that
// reads the most recent row_hash for the tenant on cold start; we ship
// the simpler in-process tail today since the next inserter recovers via
// "latest row" lookup at chain-verify time.
type AuditStore struct {
	pool *pgxpool.Pool
	now  func() time.Time

	mu        sync.Mutex
	chainTail map[string][]byte // tenant_id → last row_hash
}

func NewAuditStore(pool *pgxpool.Pool) *AuditStore {
	return &AuditStore{pool: pool, now: time.Now, chainTail: make(map[string][]byte)}
}

func (s *AuditStore) tail(ctx context.Context, tenantID string) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.chainTail[tenantID]; ok {
		return v
	}
	// Lazily seed from DB so multi-replica deploys still chain correctly
	// after a restart. Errors degrade to "starting a new chain" — the
	// verifier surfaces the gap as a critical event.
	var prev []byte
	_ = s.pool.QueryRow(ctx, `
		SELECT row_hash FROM audit_events
		 WHERE tenant_id = $1 ORDER BY created_at DESC LIMIT 1`, tenantID).Scan(&prev)
	s.chainTail[tenantID] = prev
	return prev
}

func (s *AuditStore) updateTail(tenantID string, hash []byte) {
	s.mu.Lock()
	s.chainTail[tenantID] = hash
	s.mu.Unlock()
}

func (s *AuditStore) Emit(ctx context.Context, e audit.Event) error {
	if e.CreatedAt.IsZero() {
		e.CreatedAt = s.now().UTC()
	}
	if e.Outcome == "" {
		e.Outcome = audit.OutcomeSuccess
	}
	prev := s.tail(ctx, e.TenantID)
	e.PrevHash = prev
	e.RowHash = audit.ComputeRowHash(e, prev)

	beforeJSON, _ := json.Marshal(e.Before)
	afterJSON, _ := json.Marshal(e.After)
	_, err := s.pool.Exec(ctx, `
		INSERT INTO audit_events (
			event_id, tenant_id, actor_id, actor_kind, action,
			resource_type, resource_id, before_json, after_json,
			ip, user_agent, request_id, created_at,
			session_id, outcome, error_message, policy_reason,
			http_method, http_path, http_status,
			service_name, service_version, prev_hash, row_hash
		) VALUES (
			COALESCE(NULLIF($1,'')::uuid, gen_random_uuid()),
			$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,
			$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)`,
		e.EventID, e.TenantID, e.ActorID, e.ActorKind, e.Action,
		e.ResourceType, e.ResourceID, beforeJSON, afterJSON,
		e.IP, e.UserAgent, e.RequestID, e.CreatedAt,
		e.SessionID, string(e.Outcome), e.ErrorMessage, e.PolicyReason,
		e.HTTPMethod, e.HTTPPath, e.HTTPStatus,
		e.ServiceName, e.ServiceVer, e.PrevHash, e.RowHash,
	)
	if err != nil {
		return fmt.Errorf("audit emit: %w", err)
	}
	s.updateTail(e.TenantID, e.RowHash)
	return nil
}

func (s *AuditStore) List(ctx context.Context, tenantID string, limit int) ([]audit.Event, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT event_id::text, tenant_id, actor_id, actor_kind, action,
		       resource_type, resource_id, before_json, after_json,
		       COALESCE(ip,''), COALESCE(user_agent,''), COALESCE(request_id,''), created_at,
		       COALESCE(session_id,''), COALESCE(outcome,'success'),
		       COALESCE(error_message,''), COALESCE(policy_reason,''),
		       COALESCE(http_method,''), COALESCE(http_path,''),
		       COALESCE(http_status,0),
		       COALESCE(service_name,''), COALESCE(service_version,''),
		       prev_hash, row_hash
		  FROM audit_events
		 WHERE tenant_id = $1
		 ORDER BY created_at DESC
		 LIMIT %d`, limit), tenantID)
	if err != nil {
		return nil, fmt.Errorf("audit list: %w", err)
	}
	defer rows.Close()
	out := make([]audit.Event, 0, limit)
	for rows.Next() {
		var (
			e         audit.Event
			beforeRaw []byte
			afterRaw  []byte
			outcome   string
		)
		if err := rows.Scan(&e.EventID, &e.TenantID, &e.ActorID, &e.ActorKind,
			&e.Action, &e.ResourceType, &e.ResourceID, &beforeRaw, &afterRaw,
			&e.IP, &e.UserAgent, &e.RequestID, &e.CreatedAt,
			&e.SessionID, &outcome, &e.ErrorMessage, &e.PolicyReason,
			&e.HTTPMethod, &e.HTTPPath, &e.HTTPStatus,
			&e.ServiceName, &e.ServiceVer, &e.PrevHash, &e.RowHash); err != nil {
			return nil, err
		}
		e.Outcome = audit.Outcome(outcome)
		if len(beforeRaw) > 0 {
			_ = json.Unmarshal(beforeRaw, &e.Before)
		}
		if len(afterRaw) > 0 {
			_ = json.Unmarshal(afterRaw, &e.After)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
