-- 0023_ai_provider_cloud_fields: per-kind auth context columns for the
-- cloud LLM providers added in the bedrock + vertex + azure-openai
-- expansion. Each column is optional (default '') so existing rows
-- (Anthropic / OpenAI / DeepSeek / OpenAI-compatible) keep working
-- with zero migration on the row body itself.
--
-- The columns are denormalised onto the configs row rather than a
-- per-kind side-table because (a) there's at most five of them, (b)
-- the resolver path reads ALL of them on every Build, and a join
-- would double the work, and (c) keeping the shape flat means the
-- Postgres Repo + the in-memory Repo stay structurally identical.

ALTER TABLE ai_provider_configs
    ADD COLUMN IF NOT EXISTS deployment  TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS api_version TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS region      TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS project     TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS location    TEXT NOT NULL DEFAULT '';
