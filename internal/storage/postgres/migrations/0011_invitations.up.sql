-- 0011_invitations: pending teammate invites.
--
-- One row per outstanding invite. roles is JSONB so we can attach the
-- standard role set (viewer/editor/steward/admin) without a join. The
-- token is the secret emailed to the invitee — we store it plaintext
-- because invites are single-use, 7-day-lived, and tied to one email.
-- UNIQUE(token) is the index hot-path the accept handler hits.
--
-- Composite UNIQUE(tenant_id, email, status='pending') is enforced
-- in app code instead of the schema because the "status" is computed
-- from accepted_at / revoked_at / expires_at; a partial index on
-- expires_at would have to compare against now() and Postgres rejects
-- non-IMMUTABLE expressions in index predicates.

CREATE TABLE IF NOT EXISTS invitations (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    TEXT NOT NULL,
    email        TEXT NOT NULL,
    roles        JSONB NOT NULL DEFAULT '[]'::jsonb,
    token        TEXT NOT NULL UNIQUE,
    invited_by   TEXT NOT NULL DEFAULT '',
    expires_at   TIMESTAMPTZ NOT NULL,
    accepted_at  TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_invitations_tenant_created
    ON invitations(tenant_id, created_at DESC);
