-- Incremental migration support: each Plan can declare the monotonic
-- column its runner sorts + paginates by. NULL means snapshot mode
-- (the row's only legal mode when this column is null).
ALTER TABLE migration_plans
    ADD COLUMN IF NOT EXISTS cursor_column TEXT NOT NULL DEFAULT '';
