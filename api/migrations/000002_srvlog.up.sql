-- Srvlog events hypertable, indexes, triggers, meta caches, and continuous aggregate.

-------------------------------------------------------------------------------
-- 1. Hypertable
-------------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS srvlog_events (
    id              BIGINT GENERATED ALWAYS AS IDENTITY,
    received_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    reported_at     TIMESTAMPTZ NOT NULL,
    hostname        TEXT        NOT NULL,
    fromhost_ip     INET        NOT NULL,
    programname     TEXT        NOT NULL DEFAULT '',
    msgid           TEXT        NOT NULL DEFAULT '',
    severity        SMALLINT    NOT NULL,
    facility        SMALLINT    NOT NULL,
    syslogtag       TEXT        NOT NULL DEFAULT '',
    structured_data TEXT,
    message         TEXT        NOT NULL,
    raw_message     TEXT,
    msg_pattern     TEXT        NOT NULL DEFAULT ''
) WITH (
    tsdb.hypertable,
    tsdb.partition_column = 'received_at',
    tsdb.chunk_interval = '1 day',
    tsdb.create_default_indexes = false,
    tsdb.segmentby = 'hostname',
    tsdb.orderby = 'received_at DESC'
);

-------------------------------------------------------------------------------
-- 2. Indexes
-------------------------------------------------------------------------------

-- Cursor pagination index (main access pattern).
CREATE INDEX IF NOT EXISTS idx_srvlog_received_id
    ON srvlog_events (received_at DESC, id DESC);

-- Single event lookup by ID (scans all chunks, acceptable for rare lookups).
CREATE INDEX IF NOT EXISTS idx_srvlog_id
    ON srvlog_events (id);

-- Filter indexes (compound with received_at for sort elimination).
CREATE INDEX IF NOT EXISTS idx_srvlog_host_received
    ON srvlog_events (hostname, received_at DESC);

CREATE INDEX IF NOT EXISTS idx_srvlog_severity_received
    ON srvlog_events (severity, received_at DESC, id DESC)
    WHERE severity <= 3;

CREATE INDEX IF NOT EXISTS idx_srvlog_programname
    ON srvlog_events (programname, received_at DESC);

CREATE INDEX IF NOT EXISTS idx_srvlog_facility
    ON srvlog_events (facility, received_at DESC);

CREATE INDEX IF NOT EXISTS idx_srvlog_fromhost_ip
    ON srvlog_events (fromhost_ip, received_at DESC);

CREATE INDEX IF NOT EXISTS idx_srvlog_syslogtag
    ON srvlog_events (syslogtag, received_at DESC);

-- Trigram index for ILIKE substring search on message.
CREATE INDEX IF NOT EXISTS idx_srvlog_message_trgm
    ON srvlog_events USING GIN (message gin_trgm_ops);

-------------------------------------------------------------------------------
-- 3. LISTEN/NOTIFY trigger
-------------------------------------------------------------------------------

-- Sends the row ID so the Go backend can fetch the full event by ID.
CREATE OR REPLACE FUNCTION notify_srvlog_insert()
RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('srvlog_ingest', NEW.id::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_srvlog_notify ON srvlog_events;
CREATE TRIGGER trg_srvlog_notify
    AFTER INSERT ON srvlog_events
    FOR EACH ROW EXECUTE FUNCTION notify_srvlog_insert();

-------------------------------------------------------------------------------
-- 4. Msg pattern trigger
-------------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION compute_srvlog_msg_pattern()
RETURNS trigger AS $$
BEGIN
    NEW.msg_pattern := regexp_replace(
        regexp_replace(left(NEW.message, 200), '\d{1,3}(\.\d{1,3}){3}(:\d+)?', '<ip>', 'g'),
        '\d+', '<n>', 'g'
    );
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_srvlog_msg_pattern ON srvlog_events;
CREATE TRIGGER trg_srvlog_msg_pattern
    BEFORE INSERT ON srvlog_events
    FOR EACH ROW EXECUTE FUNCTION compute_srvlog_msg_pattern();

-------------------------------------------------------------------------------
-- 5. Columnstore + retention policies
-------------------------------------------------------------------------------

CALL remove_columnstore_policy('srvlog_events');
CALL add_columnstore_policy('srvlog_events', after => INTERVAL '1 day');

SELECT add_retention_policy('srvlog_events', INTERVAL '90 days', if_not_exists => true);

-- Tune autovacuum for high-insert workload.
ALTER TABLE srvlog_events SET (
    autovacuum_vacuum_scale_factor = 0.05,
    autovacuum_analyze_scale_factor = 0.02
);

-------------------------------------------------------------------------------
-- 6. Meta caches
-------------------------------------------------------------------------------

-- Srvlog meta cache (last_seen_at tracks hostname freshness).
CREATE TABLE IF NOT EXISTS srvlog_meta_cache (
    column_name  TEXT NOT NULL,
    value        TEXT NOT NULL,
    last_seen_at TIMESTAMPTZ,
    PRIMARY KEY (column_name, value)
);

-- ON CONFLICT DO NOTHING: once a value exists, skip with no lock/WAL/dead tuple.
CREATE OR REPLACE FUNCTION cache_srvlog_meta()
RETURNS trigger AS $$
BEGIN
    INSERT INTO srvlog_meta_cache (column_name, value)
    VALUES
        ('hostname', NEW.hostname),
        ('programname', NEW.programname),
        ('syslogtag', NEW.syslogtag)
    ON CONFLICT DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_srvlog_meta_cache ON srvlog_events;
CREATE TRIGGER trg_srvlog_meta_cache
    AFTER INSERT ON srvlog_events
    FOR EACH ROW EXECUTE FUNCTION cache_srvlog_meta();

-- Srvlog facility cache.
CREATE TABLE IF NOT EXISTS srvlog_facility_cache (
    facility SMALLINT PRIMARY KEY
);

CREATE OR REPLACE FUNCTION cache_srvlog_facility()
RETURNS trigger AS $$
BEGIN
    INSERT INTO srvlog_facility_cache (facility)
    VALUES (NEW.facility)
    ON CONFLICT DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_srvlog_facility_cache ON srvlog_events;
CREATE TRIGGER trg_srvlog_facility_cache
    AFTER INSERT ON srvlog_events
    FOR EACH ROW EXECUTE FUNCTION cache_srvlog_facility();

-------------------------------------------------------------------------------
-- 7. Continuous aggregate
-------------------------------------------------------------------------------

CREATE MATERIALIZED VIEW IF NOT EXISTS srvlog_summary_hourly
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 hour', received_at) AS bucket,
    hostname,
    severity,
    count(*) AS cnt
FROM srvlog_events
GROUP BY bucket, hostname, severity
WITH NO DATA;

SELECT add_continuous_aggregate_policy('srvlog_summary_hourly',
    start_offset    => INTERVAL '90 days',
    end_offset      => INTERVAL '1 hour',
    schedule_interval => INTERVAL '1 hour',
    if_not_exists   => true
);

ALTER MATERIALIZED VIEW srvlog_summary_hourly SET (timescaledb.materialized_only = false);

-- Columnstore compression on the continuous aggregate.
ALTER MATERIALIZED VIEW srvlog_summary_hourly SET (
    timescaledb.enable_columnstore,
    timescaledb.segmentby = 'hostname, severity',
    timescaledb.orderby = 'bucket DESC'
);
CALL add_columnstore_policy('srvlog_summary_hourly', after => INTERVAL '3 days');
