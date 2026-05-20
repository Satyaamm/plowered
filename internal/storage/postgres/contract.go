package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/contract"
)

// ContractStore is the Postgres-backed contract.Store.
type ContractStore struct {
	pool *pgxpool.Pool
}

func NewContractStore(p *pgxpool.Pool) *ContractStore {
	return &ContractStore{pool: p}
}

// UpsertContract creates a contract or bumps the version on the
// existing one. UNIQUE(tenant_id, asset_id) makes the conflict path
// fire whenever a second contract for the same asset is submitted.
func (s *ContractStore) UpsertContract(ctx context.Context, c *contract.Contract) (*contract.Contract, error) {
	if c == nil {
		return nil, errors.New("contract: nil")
	}
	expCols, _ := json.Marshal(c.ExpectedColumns)
	nullThresholds, _ := json.Marshal(c.NullThresholds)
	const q = `
		INSERT INTO data_contracts
			(tenant_id, asset_id, owner_id, status, version,
			 expected_columns, freshness_seconds, null_thresholds, description)
		VALUES ($1::uuid, $2, NULLIF($3,'')::uuid, $4, 1,
		        $5::jsonb, $6, $7::jsonb, $8)
		ON CONFLICT (tenant_id, asset_id) DO UPDATE
		   SET status            = EXCLUDED.status,
		       owner_id          = EXCLUDED.owner_id,
		       expected_columns  = EXCLUDED.expected_columns,
		       freshness_seconds = EXCLUDED.freshness_seconds,
		       null_thresholds   = EXCLUDED.null_thresholds,
		       description       = EXCLUDED.description,
		       version           = data_contracts.version + 1,
		       updated_at        = now()
		RETURNING id::text, version, created_at, updated_at`
	if c.Status == "" {
		c.Status = contract.StatusActive
	}
	row := s.pool.QueryRow(ctx, q,
		c.TenantID, c.AssetID, c.OwnerID, string(c.Status),
		expCols, c.FreshnessSeconds, nullThresholds, c.Description,
	)
	if err := row.Scan(&c.ID, &c.Version, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, fmt.Errorf("upsert contract: %w", err)
	}
	return c, nil
}

func (s *ContractStore) GetContract(ctx context.Context, tenantID, id string) (*contract.Contract, error) {
	const q = `
		SELECT id::text, tenant_id::text, asset_id,
		       COALESCE(owner_id::text,''), status, version,
		       expected_columns, freshness_seconds, null_thresholds,
		       description, created_at, updated_at
		  FROM data_contracts
		 WHERE tenant_id = $1::uuid AND id = $2::uuid`
	return scanContract(s.pool.QueryRow(ctx, q, tenantID, id))
}

func (s *ContractStore) GetContractByAsset(ctx context.Context, tenantID, assetID string) (*contract.Contract, error) {
	const q = `
		SELECT id::text, tenant_id::text, asset_id,
		       COALESCE(owner_id::text,''), status, version,
		       expected_columns, freshness_seconds, null_thresholds,
		       description, created_at, updated_at
		  FROM data_contracts
		 WHERE tenant_id = $1::uuid AND asset_id = $2`
	c, err := scanContract(s.pool.QueryRow(ctx, q, tenantID, assetID))
	if errors.Is(err, contract.ErrNotFound) {
		return nil, nil
	}
	return c, err
}

func (s *ContractStore) ListContracts(ctx context.Context, tenantID string) ([]*contract.Contract, error) {
	const q = `
		SELECT id::text, tenant_id::text, asset_id,
		       COALESCE(owner_id::text,''), status, version,
		       expected_columns, freshness_seconds, null_thresholds,
		       description, created_at, updated_at
		  FROM data_contracts
		 WHERE tenant_id = $1::uuid
		 ORDER BY updated_at DESC`
	rows, err := s.pool.Query(ctx, q, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list contracts: %w", err)
	}
	defer rows.Close()
	out := []*contract.Contract{}
	for rows.Next() {
		c, err := scanContract(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *ContractStore) DeleteContract(ctx context.Context, tenantID, id string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM data_contracts WHERE tenant_id = $1::uuid AND id = $2::uuid`, tenantID, id)
	return err
}

func scanContract(row pgx.Row) (*contract.Contract, error) {
	c := &contract.Contract{}
	var statusStr string
	var expCols, nullThresholds []byte
	err := row.Scan(
		&c.ID, &c.TenantID, &c.AssetID, &c.OwnerID, &statusStr, &c.Version,
		&expCols, &c.FreshnessSeconds, &nullThresholds,
		&c.Description, &c.CreatedAt, &c.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, contract.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan contract: %w", err)
	}
	c.Status = contract.Status(statusStr)
	if len(expCols) > 0 {
		_ = json.Unmarshal(expCols, &c.ExpectedColumns)
	}
	if len(nullThresholds) > 0 {
		_ = json.Unmarshal(nullThresholds, &c.NullThresholds)
	}
	return c, nil
}

// ----- Breaches -------------------------------------------------------

func (s *ContractStore) RecordBreach(ctx context.Context, b *contract.Breach) (*contract.Breach, error) {
	if b == nil {
		return nil, errors.New("contract: nil breach")
	}
	obsJSON, _ := json.Marshal(b.Observed)
	expJSON, _ := json.Marshal(b.Expected)
	const q = `
		INSERT INTO data_contract_breaches
			(tenant_id, contract_id, asset_id, contract_version,
			 kind, severity, observed, expected, message)
		VALUES ($1::uuid, $2::uuid, $3, $4,
		        $5, $6, $7::jsonb, $8::jsonb, $9)
		RETURNING id::text, observed_at`
	row := s.pool.QueryRow(ctx, q,
		b.TenantID, b.ContractID, b.AssetID, b.ContractVersion,
		string(b.Kind), b.Severity, obsJSON, expJSON, b.Message,
	)
	if err := row.Scan(&b.ID, &b.ObservedAt); err != nil {
		return nil, fmt.Errorf("insert breach: %w", err)
	}
	return b, nil
}

func (s *ContractStore) ListBreaches(ctx context.Context, tenantID string, limit int) ([]*contract.Breach, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	const q = `
		SELECT id::text, tenant_id::text, contract_id::text, asset_id,
		       contract_version, kind, severity, observed, expected,
		       message, observed_at
		  FROM data_contract_breaches
		 WHERE tenant_id = $1::uuid
		 ORDER BY observed_at DESC
		 LIMIT $2`
	return s.queryBreaches(ctx, q, tenantID, limit)
}

func (s *ContractStore) ListBreachesForContract(ctx context.Context, tenantID, contractID string, limit int) ([]*contract.Breach, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	const q = `
		SELECT id::text, tenant_id::text, contract_id::text, asset_id,
		       contract_version, kind, severity, observed, expected,
		       message, observed_at
		  FROM data_contract_breaches
		 WHERE tenant_id = $1::uuid AND contract_id = $2::uuid
		 ORDER BY observed_at DESC
		 LIMIT $3`
	return s.queryBreaches(ctx, q, tenantID, contractID, limit)
}

func (s *ContractStore) queryBreaches(ctx context.Context, q string, args ...any) ([]*contract.Breach, error) {
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list breaches: %w", err)
	}
	defer rows.Close()
	out := []*contract.Breach{}
	for rows.Next() {
		b := &contract.Breach{}
		var kind, severity string
		var obsJSON, expJSON []byte
		var observedAt time.Time
		if err := rows.Scan(
			&b.ID, &b.TenantID, &b.ContractID, &b.AssetID,
			&b.ContractVersion, &kind, &severity, &obsJSON, &expJSON,
			&b.Message, &observedAt,
		); err != nil {
			return nil, err
		}
		b.Kind = contract.BreachKind(kind)
		b.Severity = severity
		b.ObservedAt = observedAt
		if len(obsJSON) > 0 {
			_ = json.Unmarshal(obsJSON, &b.Observed)
		}
		if len(expJSON) > 0 {
			_ = json.Unmarshal(expJSON, &b.Expected)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
