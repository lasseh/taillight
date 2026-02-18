-- Application log events hypertable, indexes, triggers, and meta cache.

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
    search_vector tsvector GENERATED ALWAYS AS (
        to_tsvector('simple', coalesce(service,'') || ' ' ||
                              coalesce(component,'') || ' ' ||
                              coalesce(host,'') || ' ' ||
                              coalesce(msg,''))
    ) STORED
) WITH (
    tsdb.hypertable,
    tsdb.partition_column     = 'received_at',
    tsdb.chunk_interval       = '1 day',
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
-- 3. Columnstore + retention policies
-------------------------------------------------------------------------------

-- Override default 7-day columnstore policy: compress chunks older than 1 day.
CALL remove_columnstore_policy('applog_events');
CALL add_columnstore_policy('applog_events', after => INTERVAL '1 day');

-- Drop chunks older than 90 days (match syslog_events retention).
SELECT add_retention_policy('applog_events', INTERVAL '90 days', if_not_exists => true);

-- Tune autovacuum for high-insert workload (match syslog_events).
ALTER TABLE applog_events SET (
    autovacuum_vacuum_scale_factor = 0.05,
    autovacuum_analyze_scale_factor = 0.02
);

-------------------------------------------------------------------------------
-- 4. Meta cache
-------------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS applog_meta_cache (
    column_name  TEXT NOT NULL,
    value        TEXT NOT NULL,
    last_seen_at TIMESTAMPTZ,
    PRIMARY KEY (column_name, value)
);

CREATE OR REPLACE FUNCTION cache_applog_meta()
RETURNS trigger AS $$
BEGIN
    INSERT INTO applog_meta_cache (column_name, value, last_seen_at)
    VALUES
        ('service', NEW.service, NULL),
        ('component', NEW.component, NULL),
        ('host', NEW.host, now())
    ON CONFLICT (column_name, value) DO UPDATE
        SET last_seen_at = CASE
            WHEN EXCLUDED.column_name = 'host' THEN now()
            ELSE applog_meta_cache.last_seen_at
        END;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_applog_meta_cache ON applog_events;
CREATE TRIGGER trg_applog_meta_cache
    AFTER INSERT ON applog_events
    FOR EACH ROW EXECUTE FUNCTION cache_applog_meta();
