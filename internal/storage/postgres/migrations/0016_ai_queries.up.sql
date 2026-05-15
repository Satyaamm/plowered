-- ai_query_executions records every Text-to-SQL run end-to-end.
-- One row per Ask; updated in place when Run executes. Status moves
-- from 'generated' (SQL produced, not yet executed) to 'executed' or
-- 'failed' after the warehouse round-trip.
--
-- Audit: this is the GDPR / SOC 2 surface. "Who ran what query
-- against which connection" must be reconstructable years later.
-- The actor (generated_by) is captured even on generation because
-- generating SQL against a tenant's schema is itself a privileged
-- action — the system prompt sees real column names.
CREATE TABLE IF NOT EXISTS ai_query_executions (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id      UUID        NOT NULL,
    connection_id  UUID        NOT NULL,
    question       TEXT        NOT NULL,
    generated_sql  TEXT        NOT NULL,
    model          TEXT        NOT NULL,
    input_tokens   INTEGER     NOT NULL DEFAULT 0,
    output_tokens  INTEGER     NOT NULL DEFAULT 0,
    generated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    generated_by   UUID        NULL,
    status         TEXT        NOT NULL DEFAULT 'generated',
    executed_at    TIMESTAMPTZ NULL,
    row_count      INTEGER     NULL,
    elapsed_ms     INTEGER     NULL,
    error          TEXT        NULL
);

CREATE INDEX IF NOT EXISTS ai_query_executions_tenant_time_idx
    ON ai_query_executions (tenant_id, generated_at DESC);
