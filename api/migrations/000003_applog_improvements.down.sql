-- Reverse applog improvements.

-- Revert cache_applog_meta() to original version (no last_seen_at tracking).
CREATE OR REPLACE FUNCTION cache_applog_meta()
RETURNS trigger AS $$
BEGIN
    INSERT INTO applog_meta_cache (column_name, value)
    VALUES
        ('service', NEW.service),
        ('component', NEW.component),
        ('host', NEW.host)
    ON CONFLICT DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Drop last_seen_at column from applog_meta_cache.
ALTER TABLE applog_meta_cache DROP COLUMN IF EXISTS last_seen_at;

-- Drop host index.
DROP INDEX IF EXISTS idx_applog_host_received;
