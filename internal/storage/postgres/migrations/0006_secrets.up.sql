-- M6: secrets vault.
--
-- Stores envelope-encrypted secret values keyed by a URN. The connections
-- table references secrets by urn; rotating the master key re-encrypts
-- every row in this table without touching the foreign-key references.
--
-- Crypto: AES-256-GCM. We store ciphertext + nonce side-by-side; the
-- 32-byte master key lives outside Postgres (PLOWERED_SECRETS_MASTER_KEY
-- env var, base64). A future release moves the master key to KMS — the
-- only thing that changes is the AESVault constructor in Go.

CREATE TABLE IF NOT EXISTS secrets (
    urn         TEXT         PRIMARY KEY,
    tenant_id   UUID         NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    nonce       BYTEA        NOT NULL,
    ciphertext  BYTEA        NOT NULL,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_secrets_tenant ON secrets (tenant_id);
