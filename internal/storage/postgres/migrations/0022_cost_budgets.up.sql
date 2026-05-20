-- cost_budgets: per-tenant monthly USD cap. One row per tenant; the
-- check runs against the rolling 30-day total. When the observed
-- amount crosses warn_at_pct or hard_at_pct, the cost.Watcher
-- publishes events.CheckFailed which the notify dispatcher routes.
--
-- Defaults are conservative: a fresh row gets no budget (NULL =
-- disabled). Operators set monthly_usd via the API; warn defaults to
-- 80% / hard to 100%.
CREATE TABLE IF NOT EXISTS cost_budgets (
    tenant_id        UUID         PRIMARY KEY,
    monthly_usd      NUMERIC(12,4) NULL,
    warn_at_pct      INTEGER      NOT NULL DEFAULT 80,
    hard_at_pct      INTEGER      NOT NULL DEFAULT 100,
    last_warned_at   TIMESTAMPTZ  NULL,
    last_hard_at     TIMESTAMPTZ  NULL,
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now()
);
