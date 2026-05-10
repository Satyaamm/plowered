-- M5: email verification
--
-- Adds the missing piece for "verified user before they can use the
-- platform". `users.email_verified_at` is the flag the login handler
-- checks; `email_verifications` holds the single-use tokens that flip it.
--
-- Tokens are stored as raw text (single-use, 24h expiry) — the threat
-- model is "stolen from the user's inbox", not "leaked from the DB."
-- If we ever extend this surface to long-lived tokens we'll switch to
-- hash-at-rest.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS email_verified_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS email_verifications (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID         NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token       TEXT         NOT NULL,
    purpose     TEXT         NOT NULL DEFAULT 'verify_email',
    expires_at  TIMESTAMPTZ  NOT NULL,
    used_at     TIMESTAMPTZ,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (token)
);

CREATE INDEX IF NOT EXISTS idx_email_verifications_active
    ON email_verifications (user_id, purpose)
    WHERE used_at IS NULL;
