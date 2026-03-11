-- Reverse syslog migration.

-- Remove policies.
SELECT remove_retention_policy('syslog_events', if_exists => true);

-- Drop triggers.
DROP TRIGGER IF EXISTS trg_syslog_facility_cache ON syslog_events;
DROP TRIGGER IF EXISTS trg_syslog_meta_cache ON syslog_events;
DROP TRIGGER IF EXISTS trg_syslog_notify ON syslog_events;

-- Drop trigger functions.
DROP FUNCTION IF EXISTS cache_syslog_facility();
DROP FUNCTION IF EXISTS cache_syslog_meta();
DROP FUNCTION IF EXISTS notify_syslog_insert();

-- Drop cache tables.
DROP TABLE IF EXISTS syslog_facility_cache;
DROP TABLE IF EXISTS syslog_meta_cache;

-- Drop hypertable (cascades indexes).
DROP TABLE IF EXISTS syslog_events;
