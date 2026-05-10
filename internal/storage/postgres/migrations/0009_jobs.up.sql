-- 0009_jobs: tenant-scoped job ledger that fronts Asynq.
--
-- Asynq itself owns the Redis-side queue, retry policy, and consumer
-- distribution. The `jobs` table is the *visibility* layer: every
-- enqueue writes one row here, workers update progress/status, the API
-- polls it. That way customers see only their tenant's jobs and a
-- restart of the worker doesn't lose audit history.

CREATE TABLE IF NOT EXISTS jobs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       TEXT NOT NULL,
    type            TEXT NOT NULL,            -- "classify:connection" | "search:reindex" | …
    status          TEXT NOT NULL DEFAULT 'queued',  -- queued | running | succeeded | failed | cancelled
    progress_pct    INTEGER NOT NULL DEFAULT 0,
    message         TEXT NOT NULL DEFAULT '', -- short freeform status (e.g. "12 / 46 tables")
    result          JSONB NOT NULL DEFAULT '{}'::jsonb,
    error_message   TEXT NOT NULL DEFAULT '',
    actor_id        TEXT NOT NULL DEFAULT '',
    resource_id     TEXT NOT NULL DEFAULT '', -- connection.id / pipeline.id / etc.
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_jobs_tenant_created ON jobs(tenant_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status) WHERE status IN ('queued','running');
