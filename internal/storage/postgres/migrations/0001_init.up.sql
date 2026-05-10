-- M1 initial schema. All tables carry tenant_id; no row may exist without one.
-- Compound indexes always lead with tenant_id so the planner can prune early.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS assets (
    id              UUID         PRIMARY KEY,
    tenant_id       TEXT         NOT NULL,
    qualified_name  TEXT         NOT NULL,
    type            TEXT         NOT NULL,
    name            TEXT         NOT NULL,
    description     TEXT         NOT NULL DEFAULT '',
    description_ai  TEXT         NOT NULL DEFAULT '',
    trust           TEXT         NOT NULL DEFAULT 'unverified',
    tags            JSONB        NOT NULL DEFAULT '[]'::jsonb,
    owners          JSONB        NOT NULL DEFAULT '[]'::jsonb,
    properties      JSONB        NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    created_by      TEXT         NOT NULL DEFAULT '',
    updated_by      TEXT         NOT NULL DEFAULT '',
    UNIQUE (tenant_id, qualified_name)
);

CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE INDEX IF NOT EXISTS idx_assets_tenant_type        ON assets (tenant_id, type);
CREATE INDEX IF NOT EXISTS idx_assets_tenant_updated_at  ON assets (tenant_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_assets_qn_trgm
    ON assets USING gin (qualified_name gin_trgm_ops);

CREATE TABLE IF NOT EXISTS edges (
    id           UUID         PRIMARY KEY,
    tenant_id    TEXT         NOT NULL,
    kind         TEXT         NOT NULL,
    source_id    UUID         NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    target_id    UUID         NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    properties   JSONB        NOT NULL DEFAULT '{}'::jsonb,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, kind, source_id, target_id)
);

CREATE INDEX IF NOT EXISTS idx_edges_tenant_source  ON edges (tenant_id, source_id, kind);
CREATE INDEX IF NOT EXISTS idx_edges_tenant_target  ON edges (tenant_id, target_id, kind);

CREATE TABLE IF NOT EXISTS tags (
    id         UUID  PRIMARY KEY,
    tenant_id  TEXT  NOT NULL,
    name       TEXT  NOT NULL,
    color      TEXT  NOT NULL DEFAULT '',
    UNIQUE (tenant_id, name)
);

CREATE TABLE IF NOT EXISTS audit_events (
    event_id      UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     TEXT         NOT NULL,
    actor_id      TEXT         NOT NULL,
    actor_kind    TEXT         NOT NULL,
    action        TEXT         NOT NULL,
    resource_type TEXT         NOT NULL,
    resource_id   TEXT         NOT NULL,
    before_json   JSONB,
    after_json    JSONB,
    ip            TEXT,
    user_agent    TEXT,
    request_id    TEXT,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_tenant_created
    ON audit_events (tenant_id, created_at DESC);
