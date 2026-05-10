package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/jobs"
)

type JobsStore struct {
	pool *pgxpool.Pool
}

func NewJobsStore(p *pgxpool.Pool) *JobsStore { return &JobsStore{pool: p} }

func (s *JobsStore) Create(ctx context.Context, j *jobs.Job) (*jobs.Job, error) {
	if j.Status == "" {
		j.Status = jobs.StatusQueued
	}
	const q = `
		INSERT INTO jobs (tenant_id, type, status, actor_id, resource_id, message)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id::text, created_at`
	if err := s.pool.QueryRow(ctx, q,
		j.TenantID, j.Type, string(j.Status), j.ActorID, j.ResourceID, j.Message,
	).Scan(&j.ID, &j.CreatedAt); err != nil {
		return nil, fmt.Errorf("create job: %w", err)
	}
	return j, nil
}

func (s *JobsStore) Get(ctx context.Context, tenantID, id string) (*jobs.Job, error) {
	const q = `
		SELECT id::text, tenant_id, type, status, progress_pct, message,
		       result, error_message, actor_id, resource_id,
		       created_at,
		       COALESCE(started_at,  '0001-01-01 00:00:00+00'::timestamptz),
		       COALESCE(finished_at, '0001-01-01 00:00:00+00'::timestamptz)
		  FROM jobs
		 WHERE tenant_id = $1 AND id = $2::uuid`
	row := s.pool.QueryRow(ctx, q, tenantID, id)
	return scanJob(row)
}

func (s *JobsStore) List(ctx context.Context, tenantID string, limit int) ([]*jobs.Job, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	const q = `
		SELECT id::text, tenant_id, type, status, progress_pct, message,
		       result, error_message, actor_id, resource_id,
		       created_at,
		       COALESCE(started_at,  '0001-01-01 00:00:00+00'::timestamptz),
		       COALESCE(finished_at, '0001-01-01 00:00:00+00'::timestamptz)
		  FROM jobs
		 WHERE tenant_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2`
	rows, err := s.pool.Query(ctx, q, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()
	var out []*jobs.Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *JobsStore) Start(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE jobs SET status = 'running', started_at = now()
		 WHERE id = $1::uuid AND status = 'queued'`, id)
	return err
}

func (s *JobsStore) Progress(ctx context.Context, id string, pct int, message string) error {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE jobs SET progress_pct = $2, message = $3
		 WHERE id = $1::uuid AND status IN ('queued','running')`, id, pct, message)
	return err
}

func (s *JobsStore) Succeed(ctx context.Context, id string, result map[string]any) error {
	body, _ := json.Marshal(result)
	_, err := s.pool.Exec(ctx, `
		UPDATE jobs
		   SET status = 'succeeded', progress_pct = 100,
		       result = $2, finished_at = now()
		 WHERE id = $1::uuid`, id, body)
	return err
}

func (s *JobsStore) Fail(ctx context.Context, id, errMessage string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE jobs
		   SET status = 'failed', error_message = $2, finished_at = now()
		 WHERE id = $1::uuid`, id, errMessage)
	return err
}

func scanJob(row rowScanner) (*jobs.Job, error) {
	var (
		j      jobs.Job
		status string
		raw    []byte
	)
	if err := row.Scan(
		&j.ID, &j.TenantID, &j.Type, &status, &j.ProgressPct, &j.Message,
		&raw, &j.ErrorMsg, &j.ActorID, &j.ResourceID,
		&j.CreatedAt, &j.StartedAt, &j.FinishedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, jobs.ErrNotFound
		}
		return nil, fmt.Errorf("scan job: %w", err)
	}
	j.Status = jobs.Status(status)
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &j.Result)
	}
	return &j, nil
}

var _ jobs.Repo = (*JobsStore)(nil)
