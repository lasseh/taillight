-- Applog improvements: host index, last_seen_at tracking in meta cache.

-------------------------------------------------------------------------------
-- 1. Index on applog_events.host for host-filtered queries
-------------------------------------------------------------------------------

CREATE INDEX IF NOT EXISTS idx_applog_host_received
    ON applog_events (host, received_at DESC);

-------------------------------------------------------------------------------
-- 2. Add last_seen_at column to applog_meta_cache
-------------------------------------------------------------------------------

ALTER TABLE applog_meta_cache ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ;

-------------------------------------------------------------------------------
-- 3. Update cache_applog_meta() to track last_seen_at for hosts
--    Mirrors the syslog_meta_cache pattern (hostname → host).
-------------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION cache_applog_meta()
RETURNS trigger AS $$
BEGIN
    INSERT INTO applog_meta_cache (column_name, value, last_seen_at)
    VALUES
        ('service', NEW.service, NULL),
        ('component', NEW.component, NULL),
        ('host', NEW.host, now())
    ON CONFLICT (column_name, value) DO UPDATE
        SET last_seen_at = CASE
            WHEN EXCLUDED.column_name = 'host' THEN now()
            ELSE applog_meta_cache.last_seen_at
        END;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
