ALTER TABLE syslog_meta_cache ADD COLUMN last_seen_at TIMESTAMPTZ;

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
