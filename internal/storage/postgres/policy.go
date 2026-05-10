package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/policy"
)

// PolicyStore is the Postgres-backed policy.RuleRepo. It also satisfies
// policy.RuleStore so the Engine can read rules without the wider surface.
type PolicyStore struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewPolicyStore(pool *pgxpool.Pool) *PolicyStore {
	return &PolicyStore{pool: pool, now: time.Now}
}

func (s *PolicyStore) RulesForResource(ctx context.Context, resourceType, resourceID, tenantID string) ([]policy.Rule, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, effect, verbs, conditions
		  FROM policy_rules
		 WHERE tenant_id = $1
		   AND (resource_type = '' OR resource_type = $2)
		   AND (resource_id   = '' OR resource_id   = $3)`,
		tenantID, resourceType, resourceID)
	if err != nil {
		return nil, fmt.Errorf("rules: %w", err)
	}
	defer rows.Close()

	out := make([]policy.Rule, 0)
	for rows.Next() {
		var (
			r          policy.Rule
			effect     string
			verbsJSON  []byte
			condsJSON  []byte
		)
		if err := rows.Scan(&r.ID, &r.TenantID, &effect, &verbsJSON, &condsJSON); err != nil {
			return nil, err
		}
		r.Effect = policy.Effect(effect)
		_ = json.Unmarshal(verbsJSON, &r.Verbs)
		_ = json.Unmarshal(condsJSON, &r.Conditions)
		out = append(out, r)
	}
	return out, rows.Err()
}

// AddRule persists a Rule and returns it (with assigned ID).
func (s *PolicyStore) AddRule(r policy.Rule) policy.Rule {
	if r.ID == "" {
		r.ID = newUUID()
	}
	verbsJSON, _ := json.Marshal(r.Verbs)
	condsJSON, _ := json.Marshal(r.Conditions)
	_, _ = s.pool.Exec(context.Background(), `
		INSERT INTO policy_rules (id, tenant_id, effect, verbs, conditions, created_at)
		VALUES ($1,$2,$3,$4,$5,$6)`,
		r.ID, r.TenantID, string(r.Effect), verbsJSON, condsJSON, s.now().UTC(),
	)
	return r
}

func (s *PolicyStore) DeleteRule(tenantID, id string) bool {
	tag, err := s.pool.Exec(context.Background(),
		`DELETE FROM policy_rules WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	if err != nil {
		return false
	}
	return tag.RowsAffected() > 0
}

func (s *PolicyStore) ListRules(tenantID string) []policy.Rule {
	rules, _ := s.RulesForResource(context.Background(), "", "", tenantID)
	return rules
}
