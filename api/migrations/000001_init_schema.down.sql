-- Reverse migration: drop all tables, triggers, functions, and policies.

-- Drop rsyslog stats.
SELECT remove_retention_policy('rsyslog_stats', if_exists => true);
DROP TABLE IF EXISTS rsyslog_stats;

-- Drop auth tables (order matters: foreign keys).
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;

-- Remove retention and compression policies (ignore errors if they don't exist).
SELECT remove_retention_policy('applog_events', if_exists => true);
SELECT remove_retention_policy('syslog_events', if_exists => true);

-- Drop triggers.
DROP TRIGGER IF EXISTS trg_applog_meta_cache ON applog_events;
DROP TRIGGER IF EXISTS trg_applog_notify ON applog_events;
DROP TRIGGER IF EXISTS trg_syslog_facility_cache ON syslog_events;
DROP TRIGGER IF EXISTS trg_syslog_meta_cache ON syslog_events;
DROP TRIGGER IF EXISTS trg_syslog_notify ON syslog_events;

-- Drop trigger functions.
DROP FUNCTION IF EXISTS cache_applog_meta();
DROP FUNCTION IF EXISTS cache_syslog_facility();
DROP FUNCTION IF EXISTS cache_syslog_meta();
DROP FUNCTION IF EXISTS notify_applog_insert();
DROP FUNCTION IF EXISTS notify_syslog_insert();

-- Drop cache tables.
DROP TABLE IF EXISTS applog_meta_cache;
DROP TABLE IF EXISTS syslog_facility_cache;
DROP TABLE IF EXISTS syslog_meta_cache;

-- Drop tables (cascades indexes).
DROP TABLE IF EXISTS applog_events;
DROP TABLE IF EXISTS juniper_syslog_ref;
DROP TABLE IF EXISTS syslog_events;

-- Note: Extensions (timescaledb, pg_trgm, pg_stat_statements) are not dropped
-- as they may be used by other schemas or require superuser privileges.
