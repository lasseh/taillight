-- Analysis reports, rsyslog stats, and taillight application metrics.

-------------------------------------------------------------------------------
-- 1. Analysis reports
-------------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS analysis_reports (
    id                BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    generated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    model             TEXT NOT NULL,
    period_start      TIMESTAMPTZ NOT NULL,
    period_end        TIMESTAMPTZ NOT NULL,
    report            TEXT NOT NULL,
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    duration_ms       BIGINT NOT NULL DEFAULT 0,
    status            TEXT NOT NULL DEFAULT 'completed'
);

CREATE INDEX IF NOT EXISTS idx_analysis_reports_generated ON analysis_reports (generated_at DESC);

-------------------------------------------------------------------------------
-- 2. rsyslog impstats telemetry
-------------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS rsyslog_stats (
    collected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    origin       TEXT        NOT NULL,
    name         TEXT        NOT NULL,
    stats        JSONB       NOT NULL
) WITH (
    tsdb.hypertable,
    tsdb.partition_column = 'collected_at',
    tsdb.chunk_interval = '1 day',
    tsdb.columnstore = true,
    tsdb.segmentby = 'origin',
    tsdb.orderby = 'collected_at DESC'
);

CALL remove_columnstore_policy('rsyslog_stats');
CALL add_columnstore_policy('rsyslog_stats', after => INTERVAL '1 day');

SELECT add_retention_policy('rsyslog_stats', INTERVAL '30 days', if_not_exists => true);

CREATE INDEX IF NOT EXISTS idx_rsyslog_stats_origin_time ON rsyslog_stats (origin, collected_at DESC);
CREATE INDEX IF NOT EXISTS idx_rsyslog_stats_name_time   ON rsyslog_stats (name, collected_at DESC);

-------------------------------------------------------------------------------
-- 3. Taillight application metrics
-------------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS taillight_metrics (
    collected_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Gauges (point-in-time values)
    sse_clients_srvlog      INTEGER NOT NULL DEFAULT 0,
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
) WITH (
    tsdb.hypertable,
    tsdb.partition_column = 'collected_at',
    tsdb.chunk_interval = '1 day',
    tsdb.columnstore = true,
    tsdb.orderby = 'collected_at DESC'
);

CALL remove_columnstore_policy('taillight_metrics');
CALL add_columnstore_policy('taillight_metrics', after => INTERVAL '1 day');
SELECT add_retention_policy('taillight_metrics', INTERVAL '30 days', if_not_exists => true);
