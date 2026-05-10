DROP TABLE IF EXISTS deleted_records;

ALTER TABLE audit_events
    DROP COLUMN IF EXISTS row_hash,
    DROP COLUMN IF EXISTS prev_hash,
    DROP COLUMN IF EXISTS service_version,
    DROP COLUMN IF EXISTS service_name,
    DROP COLUMN IF EXISTS http_status,
    DROP COLUMN IF EXISTS http_path,
    DROP COLUMN IF EXISTS http_method,
    DROP COLUMN IF EXISTS policy_reason,
    DROP COLUMN IF EXISTS error_message,
    DROP COLUMN IF EXISTS outcome,
    DROP COLUMN IF EXISTS session_id;
