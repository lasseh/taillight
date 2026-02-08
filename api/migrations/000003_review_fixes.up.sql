-- =============================================================================
-- 000003_review_fixes.up.sql
-- Applies database improvements from codebase review
-- =============================================================================

-- ---------------------------------------------------------------------------
-- H2: Remove applog LISTEN/NOTIFY trigger
-- The ingest handler already broadcasts directly via SSE broker, so the
-- trigger causes duplicate broadcasts.
-- ---------------------------------------------------------------------------
DROP TRIGGER IF EXISTS trg_applog_notify ON applog_events;
DROP FUNCTION IF EXISTS notify_applog_insert();

-- ---------------------------------------------------------------------------
-- M5: Add autovacuum tuning to applog_events
-- Matches the configuration already applied to syslog_events.
-- ---------------------------------------------------------------------------
ALTER TABLE applog_events SET (
    autovacuum_vacuum_scale_factor = 0.05,
    autovacuum_analyze_scale_factor = 0.02
);

-- ---------------------------------------------------------------------------
-- M3: Extend single-column syslog filter indexes to compound indexes
-- Queries always ORDER BY received_at DESC, so compound indexes eliminate
-- the extra sort step.
-- ---------------------------------------------------------------------------
DROP INDEX IF EXISTS idx_syslog_programname;
CREATE INDEX idx_syslog_programname ON syslog_events (programname, received_at DESC);

DROP INDEX IF EXISTS idx_syslog_facility;
CREATE INDEX idx_syslog_facility ON syslog_events (facility, received_at DESC);

DROP INDEX IF EXISTS idx_syslog_fromhost_ip;
CREATE INDEX idx_syslog_fromhost_ip ON syslog_events (fromhost_ip, received_at DESC);

DROP INDEX IF EXISTS idx_syslog_syslogtag;
CREATE INDEX idx_syslog_syslogtag ON syslog_events (syslogtag, received_at DESC);

-- ---------------------------------------------------------------------------
-- L1: Drop unused GIN index on applog attrs
-- No Go code queries attrs with JSONB operators.
-- ---------------------------------------------------------------------------
DROP INDEX IF EXISTS idx_applog_attrs;
