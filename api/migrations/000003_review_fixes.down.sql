-- =============================================================================
-- 000003_review_fixes.down.sql
-- Reverts database improvements from codebase review
-- =============================================================================

-- ---------------------------------------------------------------------------
-- L1: Recreate GIN index on applog attrs
-- ---------------------------------------------------------------------------
CREATE INDEX IF NOT EXISTS idx_applog_attrs ON applog_events USING GIN (attrs jsonb_path_ops);

-- ---------------------------------------------------------------------------
-- M3: Revert compound syslog indexes back to single-column
-- ---------------------------------------------------------------------------
DROP INDEX IF EXISTS idx_syslog_programname;
CREATE INDEX idx_syslog_programname ON syslog_events (programname);

DROP INDEX IF EXISTS idx_syslog_facility;
CREATE INDEX idx_syslog_facility ON syslog_events (facility);

DROP INDEX IF EXISTS idx_syslog_fromhost_ip;
CREATE INDEX idx_syslog_fromhost_ip ON syslog_events (fromhost_ip);

DROP INDEX IF EXISTS idx_syslog_syslogtag;
CREATE INDEX idx_syslog_syslogtag ON syslog_events (syslogtag);

-- ---------------------------------------------------------------------------
-- M5: Reset autovacuum tuning on applog_events
-- ---------------------------------------------------------------------------
ALTER TABLE applog_events RESET (
    autovacuum_vacuum_scale_factor,
    autovacuum_analyze_scale_factor
);

-- ---------------------------------------------------------------------------
-- H2: Recreate applog LISTEN/NOTIFY trigger
-- ---------------------------------------------------------------------------
CREATE OR REPLACE FUNCTION notify_applog_insert()
RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('applog_ingest', NEW.id::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_applog_notify
    AFTER INSERT ON applog_events
    FOR EACH ROW
    EXECUTE FUNCTION notify_applog_insert();
