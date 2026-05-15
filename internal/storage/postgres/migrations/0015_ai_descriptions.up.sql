-- ai_descriptions_log records every auto-description suggestion the
-- platform generated, whether the user accepted it or not. Two
-- reasons to log:
--
--   1. Audit: if a tenant ever asks "why does this column have
--      description X", we can show the model + timestamp + prompt
--      hash that produced it.
--   2. Eval: aggregate accept/discard ratios per model show us when
--      a provider's quality regresses.
--
-- The suggestion is intentionally NOT joined onto assets.description
-- — that field stays the user-edited source of truth. The "Save"
-- action on the UI copies the suggestion into asset.description_ai
-- via the existing update path.
CREATE TABLE IF NOT EXISTS ai_descriptions_log (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL,
    asset_id        UUID        NOT NULL,
    model           TEXT        NOT NULL,
    suggestion      TEXT        NOT NULL,
    input_tokens    INTEGER     NOT NULL DEFAULT 0,
    output_tokens   INTEGER     NOT NULL DEFAULT 0,
    generated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    generated_by    UUID        NULL,
    accepted        BOOLEAN     NOT NULL DEFAULT false,
    accepted_at     TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS ai_descriptions_log_asset_idx
    ON ai_descriptions_log (tenant_id, asset_id, generated_at DESC);
