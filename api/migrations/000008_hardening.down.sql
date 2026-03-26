-- Reverse 000008_hardening.up.sql

-------------------------------------------------------------------------------
-- C2. Restore meta cache triggers.
-------------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION cache_srvlog_meta()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO srvlog_meta_cache (column_name, value, last_seen_at)
    VALUES ('hostname', NEW.fromhost, now())
    ON CONFLICT (column_name, value) DO UPDATE SET last_seen_at = now()
    WHERE srvlog_meta_cache.column_name = 'hostname';

    INSERT INTO srvlog_meta_cache (column_name, value)
    VALUES ('programname', NEW.programname)
    ON CONFLICT (column_name, value) DO NOTHING;

    INSERT INTO srvlog_meta_cache (column_name, value)
    VALUES ('syslogtag', NEW.syslogtag)
    ON CONFLICT (column_name, value) DO NOTHING;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_srvlog_meta_cache
    AFTER INSERT ON srvlog_events
    FOR EACH ROW EXECUTE FUNCTION cache_srvlog_meta();

CREATE OR REPLACE FUNCTION cache_applog_meta()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO applog_meta_cache (column_name, value)
    VALUES ('service', NEW.service)
    ON CONFLICT (column_name, value) DO NOTHING;

    INSERT INTO applog_meta_cache (column_name, value)
    VALUES ('component', NEW.component)
    ON CONFLICT (column_name, value) DO NOTHING;

    INSERT INTO applog_meta_cache (column_name, value, last_seen_at)
    VALUES ('host', NEW.host, now())
    ON CONFLICT (column_name, value) DO UPDATE SET last_seen_at = now()
    WHERE applog_meta_cache.column_name = 'host';

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_applog_meta_cache
    AFTER INSERT ON applog_events
    FOR EACH ROW EXECUTE FUNCTION cache_applog_meta();

-------------------------------------------------------------------------------
-- M5. Remove index.
-------------------------------------------------------------------------------

DROP INDEX IF EXISTS idx_notif_rules_event_kind;
