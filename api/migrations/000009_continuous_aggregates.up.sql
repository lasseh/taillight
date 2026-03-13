-------------------------------------------------------------------------------
-- Continuous aggregates for syslog and applog event tables.
-- Pre-computes hourly counts per (hostname, severity) and (service, level)
-- so that summary and volume queries avoid scanning raw hypertables.
--
-- materialized_only = false (default) enables real-time aggregation:
-- materialized data is combined with raw data for the most recent period.
-------------------------------------------------------------------------------

CREATE MATERIALIZED VIEW IF NOT EXISTS syslog_summary_hourly
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 hour', received_at) AS bucket,
    hostname,
    severity,
    count(*) AS cnt
FROM syslog_events
GROUP BY bucket, hostname, severity
WITH NO DATA;

SELECT add_continuous_aggregate_policy('syslog_summary_hourly',
    start_offset    => INTERVAL '3 hours',
    end_offset      => INTERVAL '1 hour',
    schedule_interval => INTERVAL '1 hour',
    if_not_exists   => true
);

CREATE MATERIALIZED VIEW IF NOT EXISTS applog_summary_hourly
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 hour', received_at) AS bucket,
    service,
    level,
    count(*) AS cnt
FROM applog_events
GROUP BY bucket, service, level
WITH NO DATA;

SELECT add_continuous_aggregate_policy('applog_summary_hourly',
    start_offset    => INTERVAL '3 hours',
    end_offset      => INTERVAL '1 hour',
    schedule_interval => INTERVAL '1 hour',
    if_not_exists   => true
);
