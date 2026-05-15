-- asset_profiles caches per-table profile snapshots. Keyed by the
-- table asset_id so a re-crawl that changes the table's id correctly
-- invalidates the cache. Body lives as JSONB so adding new fields to
-- profile.Report later doesn't require a column migration.
CREATE TABLE IF NOT EXISTS asset_profiles (
    tenant_id       UUID        NOT NULL,
    table_asset_id  UUID        NOT NULL,
    generated_at    TIMESTAMPTZ NOT NULL,
    body            JSONB       NOT NULL,
    PRIMARY KEY (tenant_id, table_asset_id)
);

CREATE INDEX IF NOT EXISTS asset_profiles_generated_at_idx
    ON asset_profiles (tenant_id, generated_at DESC);
