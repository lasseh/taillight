-- Reverse applog migration.

-- Remove policies.
SELECT remove_retention_policy('applog_events', if_exists => true);

-- Drop trigger.
DROP TRIGGER IF EXISTS trg_applog_meta_cache ON applog_events;

-- Drop trigger function.
DROP FUNCTION IF EXISTS cache_applog_meta();

-- Drop cache table.
DROP TABLE IF EXISTS applog_meta_cache;

-- Drop hypertable (cascades indexes).
DROP TABLE IF EXISTS applog_events;
