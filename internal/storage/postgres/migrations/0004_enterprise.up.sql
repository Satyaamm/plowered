-- M4: enterprise / production-readiness schema.
-- All tables tenant-scoped except `tenants` itself.
-- Compound indexes lead with tenant_id so the planner can prune early.

-- ─────────────────────────────────────────────────────────────────────
-- 1. Tenants & users (the multi-tenancy boundary)
-- ─────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS tenants (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    slug            TEXT         NOT NULL UNIQUE,        -- url-safe, used in JWT aud
    name            TEXT         NOT NULL,
    region          TEXT         NOT NULL DEFAULT 'us-east-1',
    tier            TEXT         NOT NULL DEFAULT 'standard',  -- free|standard|enterprise|hipaa
    status          TEXT         NOT NULL DEFAULT 'active',     -- active|suspended|terminated
    encryption_key_id UUID,                              -- FK to encryption_keys (defined later)
    settings        JSONB        NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    suspended_at    TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS users (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    email           TEXT         NOT NULL,
    email_lower     TEXT         GENERATED ALWAYS AS (lower(email)) STORED,
    full_name       TEXT         NOT NULL DEFAULT '',
    avatar_url      TEXT         NOT NULL DEFAULT '',
    status          TEXT         NOT NULL DEFAULT 'active',  -- active|locked|deleted
    password_hash   TEXT         NOT NULL DEFAULT '',         -- empty when OIDC-only
    mfa_enrolled    BOOLEAN      NOT NULL DEFAULT FALSE,
    mfa_secret      TEXT         NOT NULL DEFAULT '',         -- encrypted at app layer
    last_login_at   TIMESTAMPTZ,
    last_login_ip   TEXT         NOT NULL DEFAULT '',
    locked_at       TIMESTAMPTZ,
    locked_reason   TEXT         NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (email_lower)
);

-- Many-to-many. A user can belong to multiple tenants with different roles.
CREATE TABLE IF NOT EXISTS tenant_memberships (
    tenant_id   UUID         NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    user_id     UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    roles       JSONB        NOT NULL DEFAULT '["viewer"]'::jsonb,
    groups      JSONB        NOT NULL DEFAULT '[]'::jsonb,
    invited_by  UUID         REFERENCES users(id),
    invited_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    accepted_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tenant_id, user_id)
);

CREATE INDEX IF NOT EXISTS idx_memberships_user ON tenant_memberships (user_id);

-- ─────────────────────────────────────────────────────────────────────
-- 2. Sessions & API keys (auth materials)
-- ─────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS sessions (
    id            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id     UUID         NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    ip            TEXT         NOT NULL DEFAULT '',
    user_agent    TEXT         NOT NULL DEFAULT '',
    issued_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    last_seen_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    expires_at    TIMESTAMPTZ  NOT NULL,
    revoked_at    TIMESTAMPTZ,
    revoked_reason TEXT        NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions (user_id, issued_at DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_tenant_active
    ON sessions (tenant_id, last_seen_at DESC)
    WHERE revoked_at IS NULL;

-- API keys for service-to-service / SDK access. Hashed at rest — only the
-- hash is stored; the full key is shown once at creation.
CREATE TABLE IF NOT EXISTS api_keys (
    id            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID         NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name          TEXT         NOT NULL,
    prefix        TEXT         NOT NULL,           -- first 8 chars, for "did this key make X" lookups
    key_hash      TEXT         NOT NULL,           -- bcrypt or argon2 of the full key
    scopes        JSONB        NOT NULL DEFAULT '[]'::jsonb,  -- coarse permissions
    created_by    UUID         NOT NULL REFERENCES users(id),
    last_used_at  TIMESTAMPTZ,
    last_used_ip  TEXT         NOT NULL DEFAULT '',
    expires_at    TIMESTAMPTZ,                      -- NULL = never
    revoked_at    TIMESTAMPTZ,
    revoked_by    UUID         REFERENCES users(id),
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_api_keys_prefix ON api_keys (prefix);
CREATE INDEX IF NOT EXISTS idx_api_keys_tenant_active
    ON api_keys (tenant_id) WHERE revoked_at IS NULL;

-- ─────────────────────────────────────────────────────────────────────
-- 3. Connections (the customer datasources quality checks run against)
-- ─────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS connections (
    id           UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID         NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name         TEXT         NOT NULL,
    type         TEXT         NOT NULL,            -- postgres|snowflake|bigquery|redshift|databricks|...
    config       JSONB        NOT NULL DEFAULT '{}'::jsonb,  -- host, port, db, role, warehouse, etc.
    secret_urn   TEXT         NOT NULL DEFAULT '',           -- vault path for credentials
    health       TEXT         NOT NULL DEFAULT 'unknown',    -- healthy|degraded|unreachable|unknown
    last_check_at TIMESTAMPTZ,
    created_by   UUID         NOT NULL REFERENCES users(id),
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, name)
);

CREATE INDEX IF NOT EXISTS idx_connections_tenant_type ON connections (tenant_id, type);

-- ─────────────────────────────────────────────────────────────────────
-- 4. Workspaces (an optional scope between tenant and resource)
-- ─────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS workspaces (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID         NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    slug        TEXT         NOT NULL,
    name        TEXT         NOT NULL,
    description TEXT         NOT NULL DEFAULT '',
    created_by  UUID         NOT NULL REFERENCES users(id),
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, slug)
);

CREATE TABLE IF NOT EXISTS workspace_memberships (
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id      UUID NOT NULL REFERENCES users(id)      ON DELETE CASCADE,
    roles        JSONB NOT NULL DEFAULT '["viewer"]'::jsonb,
    PRIMARY KEY (workspace_id, user_id)
);

-- ─────────────────────────────────────────────────────────────────────
-- 5. Glossary & data classifications (governance)
-- ─────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS glossary_terms (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID         NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name        TEXT         NOT NULL,
    definition  TEXT         NOT NULL DEFAULT '',
    parent_id   UUID         REFERENCES glossary_terms(id) ON DELETE SET NULL,
    status      TEXT         NOT NULL DEFAULT 'draft',  -- draft|reviewed|approved|deprecated
    owner_id    UUID         REFERENCES users(id),
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, name)
);

CREATE TABLE IF NOT EXISTS term_assignments (
    tenant_id     UUID NOT NULL REFERENCES tenants(id)        ON DELETE CASCADE,
    term_id       UUID NOT NULL REFERENCES glossary_terms(id) ON DELETE CASCADE,
    asset_id      UUID NOT NULL REFERENCES assets(id)         ON DELETE CASCADE,
    assigned_by   UUID REFERENCES users(id),
    assigned_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (term_id, asset_id)
);

CREATE TABLE IF NOT EXISTS data_classifications (
    asset_id      UUID NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    tenant_id     UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    classification TEXT NOT NULL,                       -- public|internal|confidential|pii|phi|pci
    applied_by    UUID REFERENCES users(id),
    applied_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (asset_id, classification)
);

-- ─────────────────────────────────────────────────────────────────────
-- 6. GDPR — consent + DSR (data subject requests)
-- ─────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS consent_records (
    id           UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID         NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    subject_id   TEXT         NOT NULL,                   -- the data subject (email hash, customer id, …)
    purpose      TEXT         NOT NULL,                   -- e.g. marketing, analytics, support
    legal_basis  TEXT         NOT NULL,                   -- consent|contract|legal_obligation|legitimate_interest
    granted_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    revoked_at   TIMESTAMPTZ,
    source       TEXT         NOT NULL DEFAULT 'app',     -- app|api|import
    proof        JSONB        NOT NULL DEFAULT '{}'::jsonb -- IP, UA, signed click-through
);

CREATE INDEX IF NOT EXISTS idx_consent_subject ON consent_records (tenant_id, subject_id);

CREATE TABLE IF NOT EXISTS dsr_requests (
    id            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID         NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    subject_id    TEXT         NOT NULL,
    type          TEXT         NOT NULL,                  -- access|portability|rectification|erasure|restriction
    status        TEXT         NOT NULL DEFAULT 'received', -- received|processing|completed|rejected
    received_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    due_at        TIMESTAMPTZ  NOT NULL,                  -- received + 30 days for GDPR
    completed_at  TIMESTAMPTZ,
    handled_by    UUID         REFERENCES users(id),
    notes         TEXT         NOT NULL DEFAULT '',
    artifact_urn  TEXT         NOT NULL DEFAULT ''        -- s3 path of the export bundle
);

CREATE INDEX IF NOT EXISTS idx_dsr_tenant_due ON dsr_requests (tenant_id, due_at)
    WHERE completed_at IS NULL;

-- ─────────────────────────────────────────────────────────────────────
-- 7. Legal hold — prevents deletion regardless of retention policy
-- ─────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS legal_holds (
    id            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID         NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    matter        TEXT         NOT NULL,                  -- "Acme v. Plowered #2026-04"
    reason        TEXT         NOT NULL DEFAULT '',
    scope         JSONB        NOT NULL DEFAULT '{}'::jsonb, -- e.g. { "asset_ids": [...] } or { "tags": ["pii"] }
    issued_by     UUID         NOT NULL REFERENCES users(id),
    issued_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    released_at   TIMESTAMPTZ,
    released_by   UUID         REFERENCES users(id)
);

CREATE INDEX IF NOT EXISTS idx_legal_holds_active
    ON legal_holds (tenant_id) WHERE released_at IS NULL;

-- ─────────────────────────────────────────────────────────────────────
-- 8. Idempotency keys (safe retries on every write endpoint)
-- ─────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS idempotency_keys (
    key             TEXT         NOT NULL,
    tenant_id       UUID         NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    method          TEXT         NOT NULL,
    path            TEXT         NOT NULL,
    request_hash    TEXT         NOT NULL,                -- sha256(body) — reject if mismatched
    response_status INTEGER      NOT NULL,
    response_body   JSONB,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ  NOT NULL,
    PRIMARY KEY (tenant_id, key)
);

-- ─────────────────────────────────────────────────────────────────────
-- 9. Outbox pattern (reliable cross-service events)
-- ─────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS outbox (
    id              BIGSERIAL    PRIMARY KEY,
    tenant_id       UUID         NOT NULL,
    aggregate_type  TEXT         NOT NULL,                -- pipeline_run|check_run|asset|...
    aggregate_id    TEXT         NOT NULL,
    event_type      TEXT         NOT NULL,
    payload         JSONB        NOT NULL,
    occurred_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    processed_at    TIMESTAMPTZ,
    process_attempts INTEGER     NOT NULL DEFAULT 0,
    last_error      TEXT         NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_outbox_unprocessed
    ON outbox (occurred_at) WHERE processed_at IS NULL;

-- ─────────────────────────────────────────────────────────────────────
-- 10. Encryption-key rotation log
-- ─────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS encryption_keys (
    id           UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID         REFERENCES tenants(id) ON DELETE CASCADE,
    version      INTEGER      NOT NULL,                    -- 1, 2, … for the same tenant
    algorithm    TEXT         NOT NULL DEFAULT 'AES-256-GCM',
    kms_arn      TEXT         NOT NULL,                    -- pointer; we never store key material
    fingerprint  TEXT         NOT NULL,                    -- sha256 of public component
    activated_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    rotated_at   TIMESTAMPTZ,
    retired_at   TIMESTAMPTZ,
    UNIQUE (tenant_id, version)
);

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'tenants_encryption_key_fk') THEN
        ALTER TABLE tenants
            ADD CONSTRAINT tenants_encryption_key_fk
            FOREIGN KEY (encryption_key_id) REFERENCES encryption_keys(id);
    END IF;
END $$;

-- ─────────────────────────────────────────────────────────────────────
-- 11. Feature flags (per-tenant + per-user override)
-- ─────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS feature_flags (
    id            UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    flag_key      TEXT         NOT NULL,
    tenant_id     UUID         REFERENCES tenants(id) ON DELETE CASCADE,
    user_id       UUID         REFERENCES users(id)   ON DELETE CASCADE,
    enabled       BOOLEAN      NOT NULL DEFAULT FALSE,
    rollout_pct   INTEGER      NOT NULL DEFAULT 0,         -- 0..100; only when tenant_id NULL
    description   TEXT         NOT NULL DEFAULT '',
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    -- exactly one of (tenant_id, user_id) may be set; both NULL means
    -- "global default".
    CHECK ((tenant_id IS NULL) OR (user_id IS NULL))
);

CREATE INDEX IF NOT EXISTS idx_feature_flags_lookup
    ON feature_flags (flag_key, tenant_id, user_id);

-- ─────────────────────────────────────────────────────────────────────
-- 12. Distributed leases (cron leader election, etc.)
-- ─────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS leases (
    name        TEXT         PRIMARY KEY,
    holder      TEXT         NOT NULL,                    -- pod name
    expires_at  TIMESTAMPTZ  NOT NULL,
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- ─────────────────────────────────────────────────────────────────────
-- 13. Asset embeddings (vector search, optional pgvector dependency)
-- ─────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS asset_embeddings (
    asset_id    UUID         NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    tenant_id   UUID         NOT NULL,
    model       TEXT         NOT NULL,                     -- e.g. "openai/text-embedding-3-small"
    dim         INTEGER      NOT NULL,
    embedding   JSONB        NOT NULL,                     -- swap to vector(N) once pgvector is enabled
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (asset_id, model)
);

-- ─────────────────────────────────────────────────────────────────────
-- 14. Column-level lineage (finer than the asset graph)
-- ─────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS column_lineage (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID         NOT NULL,
    source_asset_id UUID         NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    source_column   TEXT         NOT NULL,
    target_asset_id UUID         NOT NULL REFERENCES assets(id) ON DELETE CASCADE,
    target_column   TEXT         NOT NULL,
    transform       TEXT         NOT NULL DEFAULT 'identity',  -- identity|expression|aggregation
    expression      TEXT         NOT NULL DEFAULT '',
    task_run_id     UUID         REFERENCES task_runs(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (source_asset_id, source_column, target_asset_id, target_column)
);

CREATE INDEX IF NOT EXISTS idx_column_lineage_target
    ON column_lineage (tenant_id, target_asset_id, target_column);
