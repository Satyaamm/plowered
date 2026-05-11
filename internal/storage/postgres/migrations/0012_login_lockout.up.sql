-- 0012_login_lockout: tracks consecutive failed login attempts per user
-- so we can lock the account after N failures (industry standard is 5).
--
-- The reset_at column tracks "when does the count expire back to 0" —
-- a single typo 5 days ago shouldn't combine with 4 typos today to
-- trigger lockout.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS failed_login_count    INTEGER     NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS failed_login_reset_at TIMESTAMPTZ;
