-- Reverse srvlog migration.

-- Drop continuous aggregate + policy.
SELECT remove_continuous_aggregate_policy('srvlog_summary_hourly', if_exists => true);
DROP MATERIALIZED VIEW IF EXISTS srvlog_summary_hourly;

-- Remove policies.
SELECT remove_retention_policy('srvlog_events', if_exists => true);

-- Drop triggers.
DROP TRIGGER IF EXISTS trg_srvlog_facility_cache ON srvlog_events;
DROP TRIGGER IF EXISTS trg_srvlog_meta_cache ON srvlog_events;
DROP TRIGGER IF EXISTS trg_srvlog_msg_pattern ON srvlog_events;
DROP TRIGGER IF EXISTS trg_srvlog_notify ON srvlog_events;

-- Drop trigger functions.
DROP FUNCTION IF EXISTS cache_srvlog_facility();
DROP FUNCTION IF EXISTS cache_srvlog_meta();
DROP FUNCTION IF EXISTS compute_srvlog_msg_pattern();
DROP FUNCTION IF EXISTS notify_srvlog_insert();

-- Drop cache tables.
DROP TABLE IF EXISTS srvlog_facility_cache;
DROP TABLE IF EXISTS srvlog_meta_cache;

-- Drop hypertable (cascades indexes).
DROP TABLE IF EXISTS srvlog_events;
