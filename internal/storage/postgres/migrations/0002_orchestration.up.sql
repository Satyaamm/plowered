-- M2 orchestration + quality + notify + policy.
-- All tables carry tenant_id; compound indexes lead with tenant_id.

CREATE TABLE IF NOT EXISTS pipelines (
    id            UUID         PRIMARY KEY,
    tenant_id     TEXT         NOT NULL,
    name          TEXT         NOT NULL,
    description   TEXT         NOT NULL DEFAULT '',
    tasks         JSONB        NOT NULL DEFAULT '[]'::jsonb,
    schedule      JSONB,
    concurrency   INTEGER      NOT NULL DEFAULT 4,
    fail_fast     BOOLEAN      NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    created_by    TEXT         NOT NULL DEFAULT '',
    updated_by    TEXT         NOT NULL DEFAULT '',
    UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_pipelines_tenant_updated
    ON pipelines (tenant_id, updated_at DESC);

CREATE TABLE IF NOT EXISTS pipeline_runs (
    id              UUID         PRIMARY KEY,
    tenant_id       TEXT         NOT NULL,
    pipeline_id     UUID         NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
    status          TEXT         NOT NULL,
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    scheduled_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    triggered_by    TEXT         NOT NULL DEFAULT '',
    idempotency_key TEXT         NOT NULL DEFAULT '',
    last_heartbeat  TIMESTAMPTZ,
    UNIQUE (tenant_id, pipeline_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_runs_tenant_scheduled
    ON pipeline_runs (tenant_id, scheduled_at DESC);
CREATE INDEX IF NOT EXISTS idx_runs_status_heartbeat
    ON pipeline_runs (status, last_heartbeat) WHERE status = 'running';

CREATE TABLE IF NOT EXISTS task_runs (
    id            UUID         PRIMARY KEY,
    tenant_id     TEXT         NOT NULL,
    run_id        UUID         NOT NULL REFERENCES pipeline_runs(id) ON DELETE CASCADE,
    task_id       TEXT         NOT NULL,
    status        TEXT         NOT NULL,
    attempt_count INTEGER      NOT NULL DEFAULT 0,
    started_at    TIMESTAMPTZ,
    finished_at   TIMESTAMPTZ,
    error_text    TEXT         NOT NULL DEFAULT '',
    output        JSONB        NOT NULL DEFAULT '{}'::jsonb,
    dead_letter   BOOLEAN      NOT NULL DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS idx_task_runs_run ON task_runs (run_id);
CREATE INDEX IF NOT EXISTS idx_task_runs_dlq ON task_runs (tenant_id, dead_letter)
    WHERE dead_letter = TRUE;

CREATE TABLE IF NOT EXISTS quality_checks (
    id          UUID         PRIMARY KEY,
    tenant_id   TEXT         NOT NULL,
    asset_id    TEXT         NOT NULL,
    asset_qn    TEXT         NOT NULL DEFAULT '',
    name        TEXT         NOT NULL,
    type        TEXT         NOT NULL,
    config      JSONB        NOT NULL DEFAULT '{}'::jsonb,
    severity    TEXT         NOT NULL DEFAULT 'warning',
    owner       TEXT         NOT NULL DEFAULT '',
    enabled     BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_checks_tenant_asset ON quality_checks (tenant_id, asset_id);

CREATE TABLE IF NOT EXISTS quality_check_runs (
    id            UUID         PRIMARY KEY,
    tenant_id     TEXT         NOT NULL,
    check_id      UUID         NOT NULL REFERENCES quality_checks(id) ON DELETE CASCADE,
    asset_id      TEXT         NOT NULL,
    outcome       TEXT         NOT NULL,
    value         DOUBLE PRECISION NOT NULL DEFAULT 0,
    threshold     DOUBLE PRECISION NOT NULL DEFAULT 0,
    diagnostic    TEXT         NOT NULL DEFAULT '',
    properties    JSONB        NOT NULL DEFAULT '{}'::jsonb,
    severity      TEXT         NOT NULL DEFAULT 'warning',
    started_at    TIMESTAMPTZ  NOT NULL,
    finished_at   TIMESTAMPTZ  NOT NULL,
    duration_ms   BIGINT       NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_check_runs_tenant_check_started
    ON quality_check_runs (tenant_id, check_id, started_at DESC);

CREATE TABLE IF NOT EXISTS notify_channels (
    id          UUID         PRIMARY KEY,
    tenant_id   TEXT         NOT NULL,
    kind        TEXT         NOT NULL,
    name        TEXT         NOT NULL,
    config      JSONB        NOT NULL DEFAULT '{}'::jsonb,
    secret_urn  TEXT         NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, name)
);

CREATE TABLE IF NOT EXISTS notify_rules (
    id           UUID         PRIMARY KEY,
    tenant_id    TEXT         NOT NULL,
    name         TEXT         NOT NULL DEFAULT '',
    channel_id   UUID         NOT NULL REFERENCES notify_channels(id) ON DELETE CASCADE,
    event_types  JSONB        NOT NULL DEFAULT '[]'::jsonb,
    min_severity TEXT         NOT NULL DEFAULT '',
    enabled      BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notify_rules_tenant ON notify_rules (tenant_id, enabled);

CREATE TABLE IF NOT EXISTS notify_deliveries (
    id              UUID         PRIMARY KEY,
    tenant_id       TEXT         NOT NULL,
    rule_id         UUID         NOT NULL REFERENCES notify_rules(id) ON DELETE CASCADE,
    channel_id      UUID         NOT NULL REFERENCES notify_channels(id) ON DELETE CASCADE,
    event_id        TEXT         NOT NULL DEFAULT '',
    subject         TEXT         NOT NULL DEFAULT '',
    body            TEXT         NOT NULL DEFAULT '',
    idempotency_key TEXT         NOT NULL,
    status          TEXT         NOT NULL,
    attempts        INTEGER      NOT NULL DEFAULT 0,
    last_error      TEXT         NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    delivered_at    TIMESTAMPTZ,
    UNIQUE (tenant_id, idempotency_key)
);

CREATE INDEX IF NOT EXISTS idx_deliveries_tenant_created
    ON notify_deliveries (tenant_id, created_at DESC);

CREATE TABLE IF NOT EXISTS policy_rules (
    id            UUID         PRIMARY KEY,
    tenant_id     TEXT         NOT NULL,
    effect        TEXT         NOT NULL,
    verbs         JSONB        NOT NULL DEFAULT '[]'::jsonb,
    conditions    JSONB        NOT NULL DEFAULT '[]'::jsonb,
    resource_type TEXT         NOT NULL DEFAULT '',
    resource_id   TEXT         NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_policy_rules_tenant ON policy_rules (tenant_id);
CREATE INDEX IF NOT EXISTS idx_policy_rules_resource
    ON policy_rules (tenant_id, resource_type, resource_id);
