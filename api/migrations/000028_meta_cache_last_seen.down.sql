-- Restore the insert-only meta cache triggers. last_seen_at predates
-- migration 28 and stays; only its default goes.

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

CREATE OR REPLACE FUNCTION cache_netlog_meta()
RETURNS trigger AS $$
BEGIN
    INSERT INTO netlog_meta_cache (column_name, value)
    VALUES
        ('hostname', NEW.hostname),
        ('programname', NEW.programname),
        ('syslogtag', NEW.syslogtag)
    ON CONFLICT DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

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

ALTER TABLE srvlog_meta_cache ALTER COLUMN last_seen_at DROP DEFAULT;
ALTER TABLE netlog_meta_cache ALTER COLUMN last_seen_at DROP DEFAULT;
ALTER TABLE applog_meta_cache ALTER COLUMN last_seen_at DROP DEFAULT;
