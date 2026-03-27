-- Reverse netlog migration.

-- Drop continuous aggregate + policy.
SELECT remove_continuous_aggregate_policy('netlog_summary_hourly', if_exists => true);
DROP MATERIALIZED VIEW IF EXISTS netlog_summary_hourly;

-- Remove policies.
SELECT remove_retention_policy('netlog_events', if_exists => true);

-- Drop triggers.
DROP TRIGGER IF EXISTS trg_netlog_facility_cache ON netlog_events;
DROP TRIGGER IF EXISTS trg_netlog_meta_cache ON netlog_events;
DROP TRIGGER IF EXISTS trg_netlog_msg_pattern ON netlog_events;
DROP TRIGGER IF EXISTS trg_netlog_notify ON netlog_events;

-- Drop trigger functions.
DROP FUNCTION IF EXISTS cache_netlog_facility();
DROP FUNCTION IF EXISTS cache_netlog_meta();
DROP FUNCTION IF EXISTS compute_netlog_msg_pattern();
DROP FUNCTION IF EXISTS notify_netlog_insert();

-- Drop cache tables.
DROP TABLE IF EXISTS netlog_facility_cache;
DROP TABLE IF EXISTS netlog_meta_cache;

-- Drop hypertable (cascades indexes).
DROP TABLE IF EXISTS netlog_events;
