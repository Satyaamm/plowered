-- cost_records: one row per billable operation the platform performed
-- on behalf of a tenant. The schema is intentionally narrow —
-- (kind, provider, cost_usd, attributes) covers every line item we
-- currently track without forcing a join.
--
-- Kinds in v0:
--   - "ai_completion"   provider = openai | anthropic | ... (the model vendor)
--   - "warehouse_query" provider = postgres | snowflake | mysql | redshift
--
-- attributes is JSONB so the per-kind shape can evolve without a
-- migration. Conventions worth knowing:
--   - ai_completion carries {model, input_tokens, output_tokens, asset_id?, feature}
--   - warehouse_query carries {connection_id, elapsed_ms, row_count, feature}
--
-- Cost is stored as NUMERIC(14,8) so the per-token fractions used by
-- the AI provider price book don't round to zero. The query layer
-- aggregates to USD at the boundary.
CREATE TABLE IF NOT EXISTS cost_records (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID         NOT NULL,
    ts          TIMESTAMPTZ  NOT NULL DEFAULT now(),
    kind        TEXT         NOT NULL,
    provider    TEXT         NOT NULL,
    cost_usd    NUMERIC(14,8) NOT NULL DEFAULT 0,
    attributes  JSONB        NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS cost_records_tenant_ts_idx
    ON cost_records (tenant_id, ts DESC);

CREATE INDEX IF NOT EXISTS cost_records_tenant_kind_ts_idx
    ON cost_records (tenant_id, kind, ts DESC);
