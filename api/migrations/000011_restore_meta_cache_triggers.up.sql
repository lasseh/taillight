-------------------------------------------------------------------------------
-- Restore meta cache INSERT triggers removed in 000008_hardening.
--
-- Uses ON CONFLICT DO NOTHING for all columns: once a value exists in the
-- cache, subsequent inserts skip it with no row lock, no WAL write, and no
-- dead tuple. The last_seen_at column is no longer maintained here — device
-- summaries query MAX(received_at) from the events tables directly (see
-- commit 24b0d93).
-------------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION cache_srvlog_meta()
RETURNS trigger AS $$
BEGIN
    INSERT INTO srvlog_meta_cache (column_name, value)
    VALUES
        ('hostname', NEW.hostname),
        ('programname', NEW.programname),
        ('syslogtag', NEW.syslogtag)
    ON CONFLICT DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_srvlog_meta_cache ON srvlog_events;
CREATE TRIGGER trg_srvlog_meta_cache
    AFTER INSERT ON srvlog_events
    FOR EACH ROW EXECUTE FUNCTION cache_srvlog_meta();

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

DROP TRIGGER IF EXISTS trg_applog_meta_cache ON applog_events;
CREATE TRIGGER trg_applog_meta_cache
    AFTER INSERT ON applog_events
    FOR EACH ROW EXECUTE FUNCTION cache_applog_meta();
