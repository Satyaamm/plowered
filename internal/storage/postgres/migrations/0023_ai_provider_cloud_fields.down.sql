-- Down: drop the per-kind auth context columns. Safe on a row body
-- created by 0010 because those rows never populated these columns.

ALTER TABLE ai_provider_configs
    DROP COLUMN IF EXISTS deployment,
    DROP COLUMN IF EXISTS api_version,
    DROP COLUMN IF EXISTS region,
    DROP COLUMN IF EXISTS project,
    DROP COLUMN IF EXISTS location;
