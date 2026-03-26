-------------------------------------------------------------------------------
-- Ensure real-time aggregation is enabled so that queries against the
-- continuous aggregates transparently include unmaterialized data.
--
-- WITH NO DATA in migration 000009 leaves the watermark unset, which on
-- some TimescaleDB versions prevents the real-time merge from covering
-- unmaterialized data.
--
-- The initial refresh (CALL refresh_continuous_aggregate) cannot run
-- inside a transaction, so it is performed at application startup instead.
-------------------------------------------------------------------------------

ALTER MATERIALIZED VIEW srvlog_summary_hourly SET (timescaledb.materialized_only = false);
ALTER MATERIALIZED VIEW applog_summary_hourly SET (timescaledb.materialized_only = false);
