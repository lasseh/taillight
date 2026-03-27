-- Reverse applog migration.

-- Drop continuous aggregate + policy.
SELECT remove_continuous_aggregate_policy('applog_summary_hourly', if_exists => true);
DROP MATERIALIZED VIEW IF EXISTS applog_summary_hourly;

-- Remove policies.
SELECT remove_retention_policy('applog_events', if_exists => true);

-- Drop triggers.
DROP TRIGGER IF EXISTS trg_applog_meta_cache ON applog_events;
DROP TRIGGER IF EXISTS trg_applog_msg_pattern ON applog_events;

-- Drop trigger functions.
DROP FUNCTION IF EXISTS cache_applog_meta();
DROP FUNCTION IF EXISTS compute_applog_msg_pattern();

-- Drop cache table.
DROP TABLE IF EXISTS applog_meta_cache;

-- Drop hypertable (cascades indexes).
DROP TABLE IF EXISTS applog_events;
