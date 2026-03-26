-- Reverse srvlog migration.

-- Remove policies.
SELECT remove_retention_policy('srvlog_events', if_exists => true);

-- Drop triggers.
DROP TRIGGER IF EXISTS trg_srvlog_facility_cache ON srvlog_events;
DROP TRIGGER IF EXISTS trg_srvlog_meta_cache ON srvlog_events;
DROP TRIGGER IF EXISTS trg_srvlog_notify ON srvlog_events;

-- Drop trigger functions.
DROP FUNCTION IF EXISTS cache_srvlog_facility();
DROP FUNCTION IF EXISTS cache_srvlog_meta();
DROP FUNCTION IF EXISTS notify_srvlog_insert();

-- Drop cache tables.
DROP TABLE IF EXISTS srvlog_facility_cache;
DROP TABLE IF EXISTS srvlog_meta_cache;

-- Drop hypertable (cascades indexes).
DROP TABLE IF EXISTS srvlog_events;
