CREATE TABLE rsyslog_stats (
    collected_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    origin       TEXT        NOT NULL,
    name         TEXT        NOT NULL,
    stats        JSONB       NOT NULL
);

SELECT create_hypertable('rsyslog_stats', 'collected_at', chunk_time_interval => INTERVAL '1 day');

ALTER TABLE rsyslog_stats SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'origin',
    timescaledb.compress_orderby = 'collected_at DESC'
);

SELECT add_compression_policy('rsyslog_stats', INTERVAL '1 day');
SELECT add_retention_policy('rsyslog_stats', INTERVAL '30 days');

CREATE INDEX idx_rsyslog_stats_origin_time ON rsyslog_stats (origin, collected_at DESC);
CREATE INDEX idx_rsyslog_stats_name_time   ON rsyslog_stats (name, collected_at DESC);
