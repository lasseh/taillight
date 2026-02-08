CREATE TABLE IF NOT EXISTS taillight_metrics (
    collected_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Gauges (point-in-time values)
    sse_clients_syslog      INTEGER NOT NULL DEFAULT 0,
    sse_clients_applog      INTEGER NOT NULL DEFAULT 0,
    db_pool_active          INTEGER NOT NULL DEFAULT 0,
    db_pool_idle            INTEGER NOT NULL DEFAULT 0,
    db_pool_total           INTEGER NOT NULL DEFAULT 0,
    -- Counters (cumulative — compute deltas in SQL)
    events_broadcast        BIGINT NOT NULL DEFAULT 0,
    events_dropped          BIGINT NOT NULL DEFAULT 0,
    applog_events_broadcast BIGINT NOT NULL DEFAULT 0,
    applog_events_dropped   BIGINT NOT NULL DEFAULT 0,
    applog_ingest_total     BIGINT NOT NULL DEFAULT 0,
    applog_ingest_errors    BIGINT NOT NULL DEFAULT 0,
    listener_reconnects     BIGINT NOT NULL DEFAULT 0
);

SELECT create_hypertable('taillight_metrics', 'collected_at');

-- Columnstore after 1 day, retain 30 days.
CALL add_columnstore_policy('taillight_metrics', after => INTERVAL '1 day');
SELECT add_retention_policy('taillight_metrics', INTERVAL '30 days', if_not_exists => true);
