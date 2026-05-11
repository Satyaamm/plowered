ALTER TABLE users
    DROP COLUMN IF EXISTS failed_login_count,
    DROP COLUMN IF EXISTS failed_login_reset_at;
