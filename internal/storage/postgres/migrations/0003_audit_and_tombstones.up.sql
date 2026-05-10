-- M3: comprehensive audit + recycle-bin tombstone schema.
--
-- Audit (extends 0001):
--   * captures every authenticated request, not just mutations
--   * records outcome, policy decision text, session, error, service version
--   * tamper-evident hash chain (prev_hash + row_hash) — verified by a
--     daily job. Any UPDATE/DELETE on audit_events breaks the chain.
--
-- Tombstones (recycle bin):
--   * deleted_records keeps the full payload of every removed row
--   * NO automatic purge — a super_admin chooses when (or whether) to
--     permanently delete. Restore is always one click away until then.

-- ─── audit_events extensions ───────────────────────────────────────────
ALTER TABLE audit_events
    ADD COLUMN IF NOT EXISTS session_id       TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS outcome          TEXT NOT NULL DEFAULT 'success',  -- success|failure|denied
    ADD COLUMN IF NOT EXISTS error_message    TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS policy_reason    TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS http_method      TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS http_path        TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS http_status      INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS service_name     TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS service_version  TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS prev_hash        BYTEA,
    ADD COLUMN IF NOT EXISTS row_hash         BYTEA;

CREATE INDEX IF NOT EXISTS idx_audit_tenant_actor_created
    ON audit_events (tenant_id, actor_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_tenant_action_created
    ON audit_events (tenant_id, action, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_tenant_resource
    ON audit_events (tenant_id, resource_type, resource_id);
CREATE INDEX IF NOT EXISTS idx_audit_outcome
    ON audit_events (tenant_id, outcome) WHERE outcome <> 'success';

-- ─── deleted_records (recycle bin) ─────────────────────────────────────
CREATE TABLE IF NOT EXISTS deleted_records (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       TEXT         NOT NULL,
    resource_type   TEXT         NOT NULL,        -- asset|pipeline|check|policy|notify_channel|notify_rule
    resource_id     TEXT         NOT NULL,        -- original row's id
    payload         JSONB        NOT NULL,        -- full row state at deletion
    deleted_by      TEXT         NOT NULL DEFAULT '',
    deleted_kind    TEXT         NOT NULL DEFAULT 'user',  -- user|service|system
    deletion_reason TEXT         NOT NULL DEFAULT '',      -- e.g. user_action|gdpr_erasure|cascade
    request_id      TEXT         NOT NULL DEFAULT '',
    parent_tombstone_id UUID    REFERENCES deleted_records(id) ON DELETE SET NULL,
    deleted_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    restored_at     TIMESTAMPTZ,
    restored_by     TEXT         NOT NULL DEFAULT '',
    purged_at       TIMESTAMPTZ,                  -- set when super_admin permanently deletes
    purged_by       TEXT         NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_deleted_tenant_type_deleted
    ON deleted_records (tenant_id, resource_type, deleted_at DESC);

CREATE INDEX IF NOT EXISTS idx_deleted_tenant_resource
    ON deleted_records (tenant_id, resource_type, resource_id);

-- "Active" tombstones (not yet restored, not purged).
CREATE INDEX IF NOT EXISTS idx_deleted_active
    ON deleted_records (tenant_id, deleted_at DESC)
    WHERE restored_at IS NULL AND purged_at IS NULL;

-- ─── compliance hardening ──────────────────────────────────────────────
-- Note for ops: production grants the application role only INSERT on
-- audit_events (no UPDATE/DELETE). Configure via your migration runner's
-- post-step or by hand:
--
--   REVOKE UPDATE, DELETE ON audit_events FROM plowered;
--   GRANT  INSERT ON audit_events TO plowered;
--   GRANT  SELECT ON audit_events TO plowered;
--
-- This file does not enforce that — it's environment-specific.
