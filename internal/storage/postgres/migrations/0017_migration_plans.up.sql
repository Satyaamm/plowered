-- migration_plans: persistent declarations of "move X to Y". One row
-- per plan; the column_map ships as JSONB so adding transform
-- expressions later is a schema-free change.
CREATE TABLE IF NOT EXISTS migration_plans (
    id                     UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id              UUID        NOT NULL,
    name                   TEXT        NOT NULL,
    source_connection_id   UUID        NOT NULL,
    source_schema          TEXT        NOT NULL DEFAULT '',
    source_table           TEXT        NOT NULL,
    dest_connection_id     UUID        NOT NULL,
    dest_schema            TEXT        NOT NULL DEFAULT '',
    dest_table             TEXT        NOT NULL,
    column_map             JSONB       NOT NULL DEFAULT '[]'::jsonb,
    mode                   TEXT        NOT NULL DEFAULT 'snapshot',
    write_mode             TEXT        NOT NULL DEFAULT 'truncate_and_replace',
    created_by             UUID        NULL,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS migration_plans_tenant_idx
    ON migration_plans (tenant_id, created_at DESC);

-- migration_runs: one row per execution attempt. The platform never
-- mutates plan rows from a run path; reads + writes are cleanly
-- separated for auditability.
CREATE TABLE IF NOT EXISTS migration_runs (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      UUID        NOT NULL,
    plan_id        UUID        NOT NULL REFERENCES migration_plans(id) ON DELETE CASCADE,
    status         TEXT        NOT NULL DEFAULT 'running',
    started_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at    TIMESTAMPTZ NULL,
    rows_read      BIGINT      NOT NULL DEFAULT 0,
    rows_written   BIGINT      NOT NULL DEFAULT 0,
    checkpoint_uri TEXT        NULL,
    error          TEXT        NULL
);

CREATE INDEX IF NOT EXISTS migration_runs_plan_idx
    ON migration_runs (tenant_id, plan_id, started_at DESC);
