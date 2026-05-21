ALTER TABLE applog_events
    DROP COLUMN IF EXISTS api_key_id,
    DROP COLUMN IF EXISTS source_ip;
