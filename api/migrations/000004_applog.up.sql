-- Application log events hypertable, indexes, triggers, meta cache, and continuous aggregate.

-------------------------------------------------------------------------------
-- 1. Hypertable
-------------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS applog_events (
    id          BIGINT GENERATED ALWAYS AS IDENTITY,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    timestamp   TIMESTAMPTZ NOT NULL,
    level       TEXT        NOT NULL,
    service     TEXT        NOT NULL,
    component   TEXT        NOT NULL DEFAULT '',
    host        TEXT        NOT NULL DEFAULT '',
    msg         TEXT        NOT NULL,
    source      TEXT        NOT NULL DEFAULT '',
    attrs       JSONB,
    msg_pattern TEXT        NOT NULL DEFAULT '',
    search_vector tsvector GENERATED ALWAYS AS (
        to_tsvector('simple', coalesce(service,'') || ' ' ||
                              coalesce(component,'') || ' ' ||
                              coalesce(host,'') || ' ' ||
                              coalesce(msg,'') || ' ' ||
                              coalesce(attrs::text,''))
    ) STORED
) WITH (
    tsdb.hypertable,
    tsdb.partition_column     = 'received_at',
    tsdb.chunk_interval       = '1 day',
    tsdb.create_default_indexes = false,
    tsdb.columnstore          = true,
    tsdb.segmentby            = 'service',
    tsdb.orderby              = 'received_at DESC, id DESC'
);

-------------------------------------------------------------------------------
-- 2. Indexes
-------------------------------------------------------------------------------

-- Cursor pagination (keyset: received_at DESC, id DESC).
CREATE INDEX IF NOT EXISTS idx_applog_received_id ON applog_events (received_at DESC, id DESC);

-- Filter indexes.
CREATE INDEX IF NOT EXISTS idx_applog_service_received ON applog_events (service, received_at DESC);
CREATE INDEX IF NOT EXISTS idx_applog_level_received ON applog_events (level, received_at DESC);
CREATE INDEX IF NOT EXISTS idx_applog_host_received ON applog_events (host, received_at DESC);

-- Full-text search.
CREATE INDEX IF NOT EXISTS idx_applog_search ON applog_events USING GIN (search_vector);

-------------------------------------------------------------------------------
-- 3. Msg pattern trigger
-------------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION compute_applog_msg_pattern()
RETURNS trigger AS $$
BEGIN
    NEW.msg_pattern := regexp_replace(
        regexp_replace(left(NEW.msg, 200), '\d{1,3}(\.\d{1,3}){3}(:\d+)?', '<ip>', 'g'),
        '\d+', '<n>', 'g'
    );
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_applog_msg_pattern ON applog_events;
CREATE TRIGGER trg_applog_msg_pattern
    BEFORE INSERT ON applog_events
    FOR EACH ROW EXECUTE FUNCTION compute_applog_msg_pattern();

-------------------------------------------------------------------------------
-- 4. Columnstore + retention policies
-------------------------------------------------------------------------------

-- Override default 7-day columnstore policy: compress chunks older than 1 day.
CALL remove_columnstore_policy('applog_events');
CALL add_columnstore_policy('applog_events', after => INTERVAL '1 day');

-- Drop chunks older than 90 days (match srvlog_events retention).
SELECT add_retention_policy('applog_events', INTERVAL '90 days', if_not_exists => true);

-- Tune autovacuum for high-insert workload (match srvlog_events).
ALTER TABLE applog_events SET (
    autovacuum_vacuum_scale_factor = 0.05,
    autovacuum_analyze_scale_factor = 0.02
);

-------------------------------------------------------------------------------
-- 5. Meta cache
-------------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS applog_meta_cache (
    column_name  TEXT NOT NULL,
    value        TEXT NOT NULL,
    last_seen_at TIMESTAMPTZ,
    PRIMARY KEY (column_name, value)
);

-- ON CONFLICT DO NOTHING: once a value exists, skip with no lock/WAL/dead tuple.
CREATE OR REPLACE FUNCTION cache_applog_meta()
RETURNS trigger AS $$
BEGIN
    INSERT INTO applog_meta_cache (column_name, value)
    VALUES
        ('service', NEW.service),
        ('component', NEW.component),
        ('host', NEW.host)
    ON CONFLICT DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_applog_meta_cache ON applog_events;
CREATE TRIGGER trg_applog_meta_cache
    AFTER INSERT ON applog_events
    FOR EACH ROW EXECUTE FUNCTION cache_applog_meta();

-------------------------------------------------------------------------------
-- 6. Continuous aggregate
-------------------------------------------------------------------------------

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
    start_offset    => INTERVAL '90 days',
    end_offset      => INTERVAL '1 hour',
    schedule_interval => INTERVAL '1 hour',
    if_not_exists   => true
);

ALTER MATERIALIZED VIEW applog_summary_hourly SET (timescaledb.materialized_only = false);

-- Columnstore compression on the continuous aggregate.
ALTER MATERIALIZED VIEW applog_summary_hourly SET (
    timescaledb.enable_columnstore,
    timescaledb.segmentby = 'service, level',
    timescaledb.orderby = 'bucket DESC'
);
CALL add_columnstore_policy('applog_summary_hourly', after => INTERVAL '3 days');
