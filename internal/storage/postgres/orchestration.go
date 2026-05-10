package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Satyaamm/plowered/internal/core/pipeline"
	"github.com/Satyaamm/plowered/internal/storage"
)

// PipelineStore is the Postgres-backed implementation of pipeline.Repo.
type PipelineStore struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

func NewPipelineStore(pool *pgxpool.Pool) *PipelineStore {
	return &PipelineStore{pool: pool, now: time.Now}
}

func (s *PipelineStore) CreatePipeline(ctx context.Context, p *pipeline.Pipeline) (*pipeline.Pipeline, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, errors.New("pipeline: nil")
	}
	cp := *p
	cp.TenantID = tenant
	if cp.ID == "" {
		cp.ID = newUUID()
	}
	now := s.now().UTC()
	cp.CreatedAt = now
	cp.UpdatedAt = now

	tasksJSON, _ := json.Marshal(cp.Tasks)
	scheduleJSON, _ := json.Marshal(cp.Schedule)

	const q = `
		INSERT INTO pipelines (
			id, tenant_id, name, description, tasks, schedule,
			concurrency, fail_fast, created_at, updated_at, created_by, updated_by
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`
	_, err = s.pool.Exec(ctx, q,
		cp.ID, cp.TenantID, cp.Name, cp.Description, tasksJSON, scheduleJSON,
		cp.Concurrency, cp.FailFast, cp.CreatedAt, cp.UpdatedAt, cp.CreatedBy, cp.UpdatedBy,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, fmt.Errorf("pipeline name %q already exists", cp.Name)
		}
		return nil, fmt.Errorf("insert pipeline: %w", err)
	}
	return &cp, nil
}

func (s *PipelineStore) UpdatePipeline(ctx context.Context, p *pipeline.Pipeline) (*pipeline.Pipeline, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	cp := *p
	cp.TenantID = tenant
	cp.UpdatedAt = s.now().UTC()
	tasksJSON, _ := json.Marshal(cp.Tasks)
	scheduleJSON, _ := json.Marshal(cp.Schedule)

	const q = `
		UPDATE pipelines SET
			name = $3, description = $4, tasks = $5, schedule = $6,
			concurrency = $7, fail_fast = $8, updated_at = $9, updated_by = $10
		WHERE tenant_id = $1 AND id = $2`
	tag, err := s.pool.Exec(ctx, q,
		tenant, cp.ID, cp.Name, cp.Description, tasksJSON, scheduleJSON,
		cp.Concurrency, cp.FailFast, cp.UpdatedAt, cp.UpdatedBy,
	)
	if err != nil {
		return nil, fmt.Errorf("update pipeline: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return nil, pipeline.ErrNotFound
	}
	return &cp, nil
}

func (s *PipelineStore) DeletePipeline(ctx context.Context, _ /* tenantID */, id string) error {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx,
		`DELETE FROM pipelines WHERE tenant_id = $1 AND id = $2`, tenant, id)
	if err != nil {
		return fmt.Errorf("delete pipeline: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pipeline.ErrNotFound
	}
	return nil
}

func (s *PipelineStore) GetPipeline(ctx context.Context, id string) (*pipeline.Pipeline, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	row := s.pool.QueryRow(ctx, selectPipelineSQL+` WHERE tenant_id = $1 AND id = $2`, tenant, id)
	return scanPipeline(row)
}

func (s *PipelineStore) ListPipelines(ctx context.Context, _ string) ([]*pipeline.Pipeline, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx,
		selectPipelineSQL+` WHERE tenant_id = $1 ORDER BY created_at`, tenant)
	if err != nil {
		return nil, fmt.Errorf("list pipelines: %w", err)
	}
	defer rows.Close()
	out := make([]*pipeline.Pipeline, 0)
	for rows.Next() {
		p, err := scanPipeline(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *PipelineStore) CreateRun(ctx context.Context, r *pipeline.Run) (*pipeline.Run, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	cp := *r
	cp.TenantID = tenant
	if cp.ID == "" {
		cp.ID = newUUID()
	}
	if cp.Status == "" {
		cp.Status = pipeline.RunQueued
	}
	if cp.ScheduledAt.IsZero() {
		cp.ScheduledAt = s.now().UTC()
	}
	const q = `
		INSERT INTO pipeline_runs (
			id, tenant_id, pipeline_id, status, scheduled_at,
			triggered_by, idempotency_key
		) VALUES ($1,$2,$3,$4,$5,$6,$7)`
	_, err = s.pool.Exec(ctx, q,
		cp.ID, cp.TenantID, cp.PipelineID, string(cp.Status),
		cp.ScheduledAt, cp.TriggeredBy, cp.IdempotencyKey,
	)
	if err != nil {
		return nil, fmt.Errorf("insert run: %w", err)
	}
	return &cp, nil
}

func (s *PipelineStore) GetRun(ctx context.Context, id string) (*pipeline.Run, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	row := s.pool.QueryRow(ctx, selectRunSQL+` WHERE tenant_id = $1 AND id = $2`, tenant, id)
	return scanRun(row)
}

func (s *PipelineStore) UpdateRun(ctx context.Context, r *pipeline.Run) error {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE pipeline_runs SET
			status = $3, started_at = NULLIF($4, '0001-01-01 00:00:00'::timestamptz),
			finished_at = NULLIF($5, '0001-01-01 00:00:00'::timestamptz)
		WHERE tenant_id = $1 AND id = $2`,
		tenant, r.ID, string(r.Status), r.StartedAt, r.FinishedAt,
	)
	if err != nil {
		return fmt.Errorf("update run: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pipeline.ErrNotFound
	}
	return nil
}

func (s *PipelineStore) ListRuns(ctx context.Context, _ string, pipelineID string, limit int) ([]*pipeline.Run, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	q := selectRunSQL + ` WHERE tenant_id = $1`
	args := []any{tenant}
	if pipelineID != "" {
		q += ` AND pipeline_id = $2`
		args = append(args, pipelineID)
	}
	q += fmt.Sprintf(` ORDER BY scheduled_at DESC LIMIT %d`, limit)

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()
	out := make([]*pipeline.Run, 0, limit)
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *PipelineStore) CreateTaskRun(ctx context.Context, tr *pipeline.TaskRun) (*pipeline.TaskRun, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	cp := *tr
	cp.TenantID = tenant
	if cp.ID == "" {
		cp.ID = newUUID()
	}
	outJSON, _ := json.Marshal(cp.Output)
	const q = `
		INSERT INTO task_runs (
			id, tenant_id, run_id, task_id, status, attempt_count,
			started_at, finished_at, error_text, output, dead_letter
		) VALUES ($1,$2,$3,$4,$5,$6,
			NULLIF($7,'0001-01-01 00:00:00'::timestamptz),
			NULLIF($8,'0001-01-01 00:00:00'::timestamptz),
			$9,$10,$11)`
	_, err = s.pool.Exec(ctx, q,
		cp.ID, cp.TenantID, cp.RunID, cp.TaskID, string(cp.Status), cp.AttemptCount,
		cp.StartedAt, cp.FinishedAt, cp.Error, outJSON, cp.DeadLetter,
	)
	if err != nil {
		return nil, fmt.Errorf("insert task_run: %w", err)
	}
	return &cp, nil
}

func (s *PipelineStore) UpdateTaskRun(ctx context.Context, tr *pipeline.TaskRun) error {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return err
	}
	outJSON, _ := json.Marshal(tr.Output)
	tag, err := s.pool.Exec(ctx, `
		UPDATE task_runs SET
			status = $3, attempt_count = $4,
			started_at  = NULLIF($5,'0001-01-01 00:00:00'::timestamptz),
			finished_at = NULLIF($6,'0001-01-01 00:00:00'::timestamptz),
			error_text = $7, output = $8, dead_letter = $9
		WHERE tenant_id = $1 AND id = $2`,
		tenant, tr.ID, string(tr.Status), tr.AttemptCount,
		tr.StartedAt, tr.FinishedAt, tr.Error, outJSON, tr.DeadLetter,
	)
	if err != nil {
		return fmt.Errorf("update task_run: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pipeline.ErrNotFound
	}
	return nil
}

func (s *PipelineStore) ListSchedulablePipelines(ctx context.Context, tenantID string) ([]*pipeline.Pipeline, error) {
	q := selectPipelineSQL + ` WHERE schedule IS NOT NULL AND schedule->>'Enabled' = 'true' AND COALESCE(schedule->>'Cron','') <> ''`
	args := []any{}
	if tenantID != "" {
		q += ` AND tenant_id = $1`
		args = append(args, tenantID)
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list schedulable: %w", err)
	}
	defer rows.Close()
	out := make([]*pipeline.Pipeline, 0)
	for rows.Next() {
		p, err := scanPipeline(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *PipelineStore) ListStuckRuns(ctx context.Context, olderThan time.Time) ([]*pipeline.Run, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, tenant_id, pipeline_id, status,
		       COALESCE(started_at,  '0001-01-01 00:00:00+00'::timestamptz),
		       COALESCE(finished_at, '0001-01-01 00:00:00+00'::timestamptz),
		       scheduled_at, triggered_by, idempotency_key,
		       COALESCE(last_heartbeat, '0001-01-01 00:00:00+00'::timestamptz)
		  FROM pipeline_runs
		 WHERE status = 'running'
		   AND COALESCE(last_heartbeat, started_at) < $1`, olderThan)
	if err != nil {
		return nil, fmt.Errorf("list stuck: %w", err)
	}
	defer rows.Close()
	out := make([]*pipeline.Run, 0)
	for rows.Next() {
		var (
			r      pipeline.Run
			status string
		)
		if err := rows.Scan(&r.ID, &r.TenantID, &r.PipelineID, &status,
			&r.StartedAt, &r.FinishedAt, &r.ScheduledAt, &r.TriggeredBy,
			&r.IdempotencyKey, &r.LastHeartbeat); err != nil {
			return nil, err
		}
		r.Status = pipeline.RunStatus(status)
		out = append(out, &r)
	}
	return out, rows.Err()
}

func (s *PipelineStore) HeartbeatRun(ctx context.Context, runID string, at time.Time) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE pipeline_runs SET last_heartbeat = $2 WHERE id = $1`, runID, at)
	if err != nil {
		return fmt.Errorf("heartbeat run: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return pipeline.ErrNotFound
	}
	return nil
}

func (s *PipelineStore) ListTaskRuns(ctx context.Context, runID string) ([]*pipeline.TaskRun, error) {
	tenant, err := storage.TenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx,
		selectTaskRunSQL+` WHERE tenant_id = $1 AND run_id = $2 ORDER BY started_at NULLS LAST`,
		tenant, runID)
	if err != nil {
		return nil, fmt.Errorf("list task_runs: %w", err)
	}
	defer rows.Close()
	out := make([]*pipeline.TaskRun, 0)
	for rows.Next() {
		tr, err := scanTaskRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, tr)
	}
	return out, rows.Err()
}

const selectPipelineSQL = `
	SELECT id, tenant_id, name, description, tasks, schedule,
	       concurrency, fail_fast, created_at, updated_at, created_by, updated_by
	  FROM pipelines`

const selectRunSQL = `
	SELECT id, tenant_id, pipeline_id, status,
	       COALESCE(started_at,  '0001-01-01 00:00:00+00'::timestamptz),
	       COALESCE(finished_at, '0001-01-01 00:00:00+00'::timestamptz),
	       scheduled_at, triggered_by, idempotency_key
	  FROM pipeline_runs`

const selectTaskRunSQL = `
	SELECT id, tenant_id, run_id, task_id, status, attempt_count,
	       COALESCE(started_at,  '0001-01-01 00:00:00+00'::timestamptz),
	       COALESCE(finished_at, '0001-01-01 00:00:00+00'::timestamptz),
	       error_text, output, dead_letter
	  FROM task_runs`

func scanPipeline(row rowScanner) (*pipeline.Pipeline, error) {
	var (
		p              pipeline.Pipeline
		tasksJSON      []byte
		scheduleJSON   []byte
	)
	err := row.Scan(
		&p.ID, &p.TenantID, &p.Name, &p.Description, &tasksJSON, &scheduleJSON,
		&p.Concurrency, &p.FailFast, &p.CreatedAt, &p.UpdatedAt, &p.CreatedBy, &p.UpdatedBy,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, pipeline.ErrNotFound
		}
		return nil, err
	}
	_ = json.Unmarshal(tasksJSON, &p.Tasks)
	if len(scheduleJSON) > 0 && string(scheduleJSON) != "null" {
		_ = json.Unmarshal(scheduleJSON, &p.Schedule)
	}
	return &p, nil
}

func scanRun(row rowScanner) (*pipeline.Run, error) {
	var (
		r      pipeline.Run
		status string
	)
	err := row.Scan(
		&r.ID, &r.TenantID, &r.PipelineID, &status,
		&r.StartedAt, &r.FinishedAt,
		&r.ScheduledAt, &r.TriggeredBy, &r.IdempotencyKey,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, pipeline.ErrNotFound
		}
		return nil, err
	}
	r.Status = pipeline.RunStatus(status)
	return &r, nil
}

func scanTaskRun(row rowScanner) (*pipeline.TaskRun, error) {
	var (
		tr        pipeline.TaskRun
		status    string
		outputRaw []byte
	)
	err := row.Scan(
		&tr.ID, &tr.TenantID, &tr.RunID, &tr.TaskID, &status, &tr.AttemptCount,
		&tr.StartedAt, &tr.FinishedAt, &tr.Error, &outputRaw, &tr.DeadLetter,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, pipeline.ErrNotFound
		}
		return nil, err
	}
	tr.Status = pipeline.TaskStatus(status)
	if len(outputRaw) > 0 {
		_ = json.Unmarshal(outputRaw, &tr.Output)
	}
	return &tr, nil
}
