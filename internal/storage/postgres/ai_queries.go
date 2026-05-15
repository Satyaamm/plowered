package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/asker"
)

// AIQueryStore is the Postgres implementation of asker.Log. Each
// generation creates a row; Run updates it in place.
type AIQueryStore struct {
	pool *pgxpool.Pool
}

func NewAIQueryStore(p *pgxpool.Pool) *AIQueryStore {
	return &AIQueryStore{pool: p}
}

func (s *AIQueryStore) RecordGenerated(ctx context.Context, p asker.RecordGeneratedParams) error {
	if p.Generation == nil {
		return errors.New("ai_queries: nil generation")
	}
	const q = `
		INSERT INTO ai_query_executions
			(tenant_id, connection_id, question, generated_sql,
			 model, input_tokens, output_tokens, generated_by, status)
		VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, NULLIF($8,'')::uuid, 'generated')
		RETURNING id::text`
	row := s.pool.QueryRow(ctx, q,
		p.TenantID, p.ConnectionID, p.Question, p.Generation.GeneratedSQL,
		p.Generation.Model, p.Generation.InputTokens, p.Generation.OutputTokens, p.GeneratedBy,
	)
	if err := row.Scan(&p.Generation.ExecutionID); err != nil {
		return fmt.Errorf("insert ai_query_executions: %w", err)
	}
	return nil
}

func (s *AIQueryStore) GetExecution(ctx context.Context, tenantID, executionID string) (*asker.Execution, error) {
	const q = `
		SELECT id::text, tenant_id::text, connection_id::text, question,
		       generated_sql, model, status, generated_at,
		       executed_at, row_count, elapsed_ms, error
		  FROM ai_query_executions
		 WHERE tenant_id = $1::uuid AND id = $2::uuid`
	row := s.pool.QueryRow(ctx, q, tenantID, executionID)
	e := &asker.Execution{}
	var execAt *time.Time
	var rowCount *int
	var elapsedMs *int64
	var errStr *string
	err := row.Scan(
		&e.ID, &e.TenantID, &e.ConnectionID, &e.Question,
		&e.GeneratedSQL, &e.Model, &e.Status, &e.GeneratedAt,
		&execAt, &rowCount, &elapsedMs, &errStr,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("ai_queries: execution %s not found", executionID)
	}
	if err != nil {
		return nil, err
	}
	e.ExecutedAt = execAt
	e.RowCount = rowCount
	e.ElapsedMs = elapsedMs
	e.Error = errStr
	return e, nil
}

func (s *AIQueryStore) RecordExecuted(ctx context.Context, tenantID, executionID string, rowCount int, elapsedMs int64, errStr string) error {
	status := "executed"
	if errStr != "" {
		status = "failed"
	}
	const q = `
		UPDATE ai_query_executions
		   SET status      = $3,
		       executed_at = now(),
		       row_count   = $4,
		       elapsed_ms  = $5,
		       error       = NULLIF($6,'')
		 WHERE tenant_id = $1::uuid AND id = $2::uuid`
	_, err := s.pool.Exec(ctx, q, tenantID, executionID, status, rowCount, elapsedMs, errStr)
	return err
}
