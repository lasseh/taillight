-------------------------------------------------------------------------------
-- Ensure real-time aggregation is enabled and seed the watermark so that
-- queries against the continuous aggregates return data immediately after
-- a fresh deploy (before the hourly refresh policy runs).
--
-- WITH NO DATA in migration 000009 leaves the watermark unset, which on
-- some TimescaleDB versions prevents the real-time merge from covering
-- unmaterialized data.  An explicit refresh establishes the watermark and
-- materializes whatever rows exist so far.
-------------------------------------------------------------------------------

ALTER MATERIALIZED VIEW syslog_summary_hourly SET (timescaledb.materialized_only = false);
ALTER MATERIALIZED VIEW applog_summary_hourly SET (timescaledb.materialized_only = false);

CALL refresh_continuous_aggregate('syslog_summary_hourly', NULL, now());
CALL refresh_continuous_aggregate('applog_summary_hourly', NULL, now());
