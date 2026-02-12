-- Restore original trigger (DO NOTHING behavior)
CREATE OR REPLACE FUNCTION cache_syslog_meta()
RETURNS trigger AS $$
BEGIN
    INSERT INTO syslog_meta_cache (column_name, value)
    VALUES
        ('hostname', NEW.hostname),
        ('programname', NEW.programname),
        ('syslogtag', NEW.syslogtag)
    ON CONFLICT DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

ALTER TABLE syslog_meta_cache DROP COLUMN last_seen_at;
