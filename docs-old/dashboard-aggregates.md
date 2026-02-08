# Dashboard & Graph Opportunities

Continuous aggregates that can be added to the TimescaleDB schema when
building dashboards or Grafana panels.

## Hourly event counts per host and severity

Useful for: host activity heatmaps, severity distribution over time,
message rate sparklines.

```sql
CREATE MATERIALIZED VIEW syslog_hourly
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 hour', received_at) AS bucket,
    hostname,
    severity,
    COUNT(*) AS event_count
FROM syslog_events
GROUP BY bucket, hostname, severity;

SELECT add_continuous_aggregate_policy('syslog_hourly',
    start_offset => INTERVAL '90 days',
    end_offset   => INTERVAL '15 minutes',
    schedule_interval => INTERVAL '15 minutes');

ALTER MATERIALIZED VIEW syslog_hourly SET (
    timescaledb.enable_columnstore = true,
    timescaledb.segmentby = 'hostname',
    timescaledb.orderby = 'bucket DESC'
);
CALL add_columnstore_policy('syslog_hourly', after => INTERVAL '3 days');
```

### Example queries

```sql
-- Messages per hour for last 24h (all hosts)
SELECT bucket, SUM(event_count) AS total
FROM syslog_hourly
WHERE bucket >= now() - INTERVAL '24 hours'
GROUP BY bucket ORDER BY bucket;

-- Top 10 noisiest hosts in last 7 days
SELECT hostname, SUM(event_count) AS total
FROM syslog_hourly
WHERE bucket >= now() - INTERVAL '7 days'
GROUP BY hostname ORDER BY total DESC LIMIT 10;

-- Severity breakdown for a specific host
SELECT severity, SUM(event_count) AS total
FROM syslog_hourly
WHERE hostname = 'core-rtr-01' AND bucket >= now() - INTERVAL '7 days'
GROUP BY severity ORDER BY severity;
```

## Daily event counts per program and facility

Useful for: identifying noisy daemons, tracking facility distribution trends.

```sql
CREATE MATERIALIZED VIEW syslog_daily_programs
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 day', received_at) AS bucket,
    programname,
    facility,
    COUNT(*) AS event_count,
    MIN(severity) AS min_severity
FROM syslog_events
GROUP BY bucket, programname, facility;

SELECT add_continuous_aggregate_policy('syslog_daily_programs',
    start_offset => INTERVAL '90 days',
    end_offset   => INTERVAL '1 hour',
    schedule_interval => INTERVAL '1 hour');
```

## Error rate tracking

Useful for: alerting on error spikes, SLO dashboards.

```sql
CREATE MATERIALIZED VIEW syslog_error_rate
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('5 minutes', received_at) AS bucket,
    hostname,
    COUNT(*) FILTER (WHERE severity <= 3) AS error_count,
    COUNT(*) AS total_count
FROM syslog_events
GROUP BY bucket, hostname;

SELECT add_continuous_aggregate_policy('syslog_error_rate',
    start_offset => INTERVAL '90 days',
    end_offset   => INTERVAL '5 minutes',
    schedule_interval => INTERVAL '5 minutes');
```

### Example queries

```sql
-- Error ratio per host, last hour
SELECT hostname,
       SUM(error_count) AS errors,
       SUM(total_count) AS total,
       ROUND(100.0 * SUM(error_count) / NULLIF(SUM(total_count), 0), 2) AS error_pct
FROM syslog_error_rate
WHERE bucket >= now() - INTERVAL '1 hour'
GROUP BY hostname
ORDER BY error_pct DESC;
```

## Grafana panel ideas

- **Message rate**: time series of `SUM(event_count)` from `syslog_hourly`
- **Host heatmap**: hostname x hour grid colored by event count
- **Severity pie chart**: distribution from `syslog_hourly` filtered by time range
- **Error spike alerts**: threshold on `error_pct` from `syslog_error_rate`
- **Top talkers table**: hostname ranked by total count, last 24h
- **Program breakdown**: stacked bar chart from `syslog_daily_programs`

## Aggregate retention

Keep aggregates longer than raw data (90 days):

```sql
SELECT add_retention_policy('syslog_hourly', INTERVAL '1 year');
SELECT add_retention_policy('syslog_daily_programs', INTERVAL '2 years');
SELECT add_retention_policy('syslog_error_rate', INTERVAL '1 year');
```

## Performance indexes on aggregates

```sql
CREATE INDEX idx_hourly_host_bucket ON syslog_hourly (hostname, bucket DESC);
CREATE INDEX idx_error_rate_host_bucket ON syslog_error_rate (hostname, bucket DESC);
CREATE INDEX idx_daily_program_bucket ON syslog_daily_programs (programname, bucket DESC);
```
