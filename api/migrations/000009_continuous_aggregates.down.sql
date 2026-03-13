-- Reverse 000009_continuous_aggregates.up.sql

SELECT remove_continuous_aggregate_policy('applog_summary_hourly', if_exists => true);
DROP MATERIALIZED VIEW IF EXISTS applog_summary_hourly;

SELECT remove_continuous_aggregate_policy('syslog_summary_hourly', if_exists => true);
DROP MATERIALIZED VIEW IF EXISTS syslog_summary_hourly;
