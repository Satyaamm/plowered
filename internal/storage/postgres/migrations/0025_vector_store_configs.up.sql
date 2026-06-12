-- 0025_vector_store_configs: tenant-level vector store choice. Mirrors
-- ai_provider_configs in shape so the Settings UI + the secrets vault
-- pattern stay symmetric.
--
-- One config row per (tenant, kind) — a tenant might run multiple
-- vector stores (e.g. pgvector for ad-hoc, Pinecone for prod) and
-- the is_primary flag picks the default the search.Indexer +
-- describer write to. The unique-partial-index pattern from
-- ai_provider_configs enforces "one primary per tenant" at the DB
-- level so a race between two admins can't leave the tenant with two
-- defaults.

CREATE TABLE IF NOT EXISTS vector_store_configs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       TEXT NOT NULL,
    kind            TEXT NOT NULL,                  -- pgvector | memory | pinecone | weaviate | qdrant
    name            TEXT NOT NULL,
    endpoint        TEXT NOT NULL DEFAULT '',
    index_name      TEXT NOT NULL DEFAULT '',       -- pinecone
    class_name      TEXT NOT NULL DEFAULT '',       -- weaviate
    collection      TEXT NOT NULL DEFAULT '',       -- qdrant
    dimension       INT  NOT NULL DEFAULT 0,
    secret_urn      TEXT NOT NULL DEFAULT '',
    is_primary      BOOLEAN NOT NULL DEFAULT FALSE,
    last_tested_at  TIMESTAMPTZ,
    last_test_ok    BOOLEAN NOT NULL DEFAULT FALSE,
    last_test_error TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_vector_store_configs_tenant
    ON vector_store_configs(tenant_id, created_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS uq_vector_store_primary
    ON vector_store_configs(tenant_id)
    WHERE is_primary;
