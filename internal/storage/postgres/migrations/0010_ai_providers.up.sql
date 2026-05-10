-- 0010_ai_providers: per-tenant BYOM provider configurations.
--
-- One row per (tenant, model) configuration. The API key itself never
-- lives here — only the secret URN that resolves into the encrypted
-- vault. capability is denormalised onto the row so the resolver can
-- pick "the chat default" or "the embed default" without re-parsing the
-- model name; the (tenant, capability, is_primary) partial unique index
-- enforces "exactly one primary per (tenant, capability)" at the DB
-- level so a race between two admins can't leave the tenant with two
-- defaults.

CREATE TABLE IF NOT EXISTS ai_provider_configs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       TEXT NOT NULL,
    kind            TEXT NOT NULL,
    name            TEXT NOT NULL,
    model           TEXT NOT NULL,
    base_url        TEXT NOT NULL DEFAULT '',
    secret_urn      TEXT NOT NULL,
    capability      TEXT NOT NULL,
    is_primary      BOOLEAN NOT NULL DEFAULT FALSE,
    last_tested_at  TIMESTAMPTZ,
    last_test_ok    BOOLEAN NOT NULL DEFAULT FALSE,
    last_test_error TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ai_provider_configs_tenant
    ON ai_provider_configs(tenant_id, created_at DESC);

-- One primary per (tenant, capability). Partial index so non-primary
-- rows do not consume the unique slot.
CREATE UNIQUE INDEX IF NOT EXISTS uq_ai_provider_primary
    ON ai_provider_configs(tenant_id, capability)
    WHERE is_primary;
