-- task_run_logs is the durable audit of what each pipeline executor said
-- as it ran. Rows append-only; the SSE endpoint tails by `id` so live
-- consumers see new lines without having to scan the whole table.
CREATE TABLE IF NOT EXISTS task_run_logs (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   TEXT NOT NULL,
    run_id      UUID NOT NULL,
    task_run_id UUID,
    task_id     TEXT NOT NULL DEFAULT '',
    level       TEXT NOT NULL DEFAULT 'info',
    line        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- (run_id, id) is the primary tail key — each SSE consumer remembers the
-- last id they saw and asks for `id > $last AND run_id = $run`.
CREATE INDEX IF NOT EXISTS idx_task_run_logs_run ON task_run_logs(run_id, id);

-- Tenant-scoped scans for "all logs for this tenant in the last hour"
-- type queries. Cheap because most queries also filter by run_id.
CREATE INDEX IF NOT EXISTS idx_task_run_logs_tenant ON task_run_logs(tenant_id, created_at DESC);
