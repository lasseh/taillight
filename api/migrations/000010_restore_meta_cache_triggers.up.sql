-------------------------------------------------------------------------------
-- Restore meta cache INSERT triggers removed in 000008_hardening.
-- The per-row overhead is negligible compared to the hypertable INSERT,
-- and the always-current cache avoids expensive DISTINCT scans.
-------------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION cache_syslog_meta()
RETURNS trigger AS $$
BEGIN
    INSERT INTO syslog_meta_cache (column_name, value, last_seen_at)
    VALUES
        ('hostname', NEW.hostname, now()),
        ('programname', NEW.programname, NULL),
        ('syslogtag', NEW.syslogtag, NULL)
    ON CONFLICT (column_name, value) DO UPDATE
        SET last_seen_at = CASE
            WHEN EXCLUDED.column_name = 'hostname' THEN now()
            ELSE syslog_meta_cache.last_seen_at
        END;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_syslog_meta_cache ON syslog_events;
CREATE TRIGGER trg_syslog_meta_cache
    AFTER INSERT ON syslog_events
    FOR EACH ROW EXECUTE FUNCTION cache_syslog_meta();

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

DROP TRIGGER IF EXISTS trg_applog_meta_cache ON applog_events;
CREATE TRIGGER trg_applog_meta_cache
    AFTER INSERT ON applog_events
    FOR EACH ROW EXECUTE FUNCTION cache_applog_meta();
