-- data_contracts: producer-declared guarantees on a catalogued asset.
-- One active contract per (tenant, asset). New versions overwrite the
-- previous spec in place — version + updated_at on the row capture
-- evolution history without the join overhead of a separate versions
-- table. The audit trail for "what was the contract last Tuesday"
-- lives on the breach rows (each breach records the contract version
-- it was evaluated against).
CREATE TABLE IF NOT EXISTS data_contracts (
    id                   UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            UUID         NOT NULL,
    asset_id             TEXT         NOT NULL,
    owner_id             UUID         NULL,
    status               TEXT         NOT NULL DEFAULT 'active', -- active | suspended | deprecated
    version              INTEGER      NOT NULL DEFAULT 1,
    expected_columns     JSONB        NOT NULL DEFAULT '[]'::jsonb,
    freshness_seconds    INTEGER      NOT NULL DEFAULT 0,        -- 0 = no freshness check
    null_thresholds      JSONB        NOT NULL DEFAULT '{}'::jsonb, -- {column: max_null_fraction}
    description          TEXT         NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, asset_id)
);

CREATE INDEX IF NOT EXISTS data_contracts_tenant_status_idx
    ON data_contracts (tenant_id, status, updated_at DESC);

-- data_contract_breaches: append-only record of every check that
-- detected a violation. The contract_version snapshot lets us
-- reconstruct what the rule was at evaluation time even after the
-- contract has been updated.
CREATE TABLE IF NOT EXISTS data_contract_breaches (
    id                UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID         NOT NULL,
    contract_id       UUID         NOT NULL REFERENCES data_contracts(id) ON DELETE CASCADE,
    asset_id          TEXT         NOT NULL,
    contract_version  INTEGER      NOT NULL,
    kind              TEXT         NOT NULL,    -- schema_drift | freshness | null_threshold
    severity          TEXT         NOT NULL DEFAULT 'error', -- info | warning | error | critical
    observed          JSONB        NOT NULL DEFAULT '{}'::jsonb,
    expected          JSONB        NOT NULL DEFAULT '{}'::jsonb,
    message           TEXT         NOT NULL DEFAULT '',
    observed_at       TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS data_contract_breaches_tenant_observed_idx
    ON data_contract_breaches (tenant_id, observed_at DESC);

CREATE INDEX IF NOT EXISTS data_contract_breaches_contract_idx
    ON data_contract_breaches (tenant_id, contract_id, observed_at DESC);
