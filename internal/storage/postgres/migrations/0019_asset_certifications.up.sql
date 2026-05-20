-- asset_certifications: every proposed/approved/rejected/revoked
-- certification decision for an asset. The latest non-terminal row
-- per asset is the asset's current certification state. History is
-- preserved so an auditor can answer "when was this certified, by
-- whom, and why".
--
-- Why a separate table rather than columns on assets:
--   1. Reviewers need a cross-asset queue ("show me everything
--      waiting on my approval"). That's one index away here; on
--      assets it would require filtering by a status column.
--   2. Justification + review_note are free-text and grow as the
--      catalog scales — keeping them off the hot asset row keeps the
--      common-case asset fetch small.
--   3. The asset itself never mutates on a certification change, so
--      caching layers around the catalog stay valid.
CREATE TABLE IF NOT EXISTS asset_certifications (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL,
    asset_id        TEXT        NOT NULL,
    status          TEXT        NOT NULL,  -- proposed | certified | rejected | revoked
    proposed_by     UUID        NULL,
    proposed_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    reviewed_by     UUID        NULL,
    reviewed_at     TIMESTAMPTZ NULL,
    justification   TEXT        NOT NULL DEFAULT '',
    review_note     TEXT        NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS asset_certifications_asset_idx
    ON asset_certifications (tenant_id, asset_id, proposed_at DESC);

CREATE INDEX IF NOT EXISTS asset_certifications_status_idx
    ON asset_certifications (tenant_id, status, proposed_at DESC);
