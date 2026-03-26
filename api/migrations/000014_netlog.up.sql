-- Netlog feed: hypertable, indexes, triggers, caches, aggregates, metrics.

-------------------------------------------------------------------------------
-- 1. Hypertable
-------------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS netlog_events (
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
CREATE INDEX IF NOT EXISTS idx_netlog_received_id
    ON netlog_events (received_at DESC, id DESC);

-- Single event lookup by ID (scans all chunks, acceptable for rare lookups).
CREATE INDEX IF NOT EXISTS idx_netlog_id
    ON netlog_events (id);

-- Filter indexes (compound with received_at for sort elimination).
CREATE INDEX IF NOT EXISTS idx_netlog_host_received
    ON netlog_events (hostname, received_at DESC);

CREATE INDEX IF NOT EXISTS idx_netlog_severity_received
    ON netlog_events (severity, received_at DESC, id DESC)
    WHERE severity <= 3;

CREATE INDEX IF NOT EXISTS idx_netlog_programname
    ON netlog_events (programname, received_at DESC);

CREATE INDEX IF NOT EXISTS idx_netlog_facility
    ON netlog_events (facility, received_at DESC);

CREATE INDEX IF NOT EXISTS idx_netlog_fromhost_ip
    ON netlog_events (fromhost_ip, received_at DESC);

CREATE INDEX IF NOT EXISTS idx_netlog_syslogtag
    ON netlog_events (syslogtag, received_at DESC);

-- Trigram index for ILIKE substring search on message.
CREATE INDEX IF NOT EXISTS idx_netlog_message_trgm
    ON netlog_events USING GIN (message gin_trgm_ops);

-------------------------------------------------------------------------------
-- 3. Autovacuum tuning
-------------------------------------------------------------------------------

-- Tune autovacuum for high-insert workload.
ALTER TABLE netlog_events SET (
    autovacuum_vacuum_scale_factor = 0.05,
    autovacuum_analyze_scale_factor = 0.02
);

-------------------------------------------------------------------------------
-- 4. LISTEN/NOTIFY trigger
-------------------------------------------------------------------------------

-- Sends the row ID so the Go backend can fetch the full event by ID.
CREATE OR REPLACE FUNCTION notify_netlog_insert()
RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('netlog_ingest', NEW.id::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_netlog_notify ON netlog_events;
CREATE TRIGGER trg_netlog_notify
    AFTER INSERT ON netlog_events
    FOR EACH ROW EXECUTE FUNCTION notify_netlog_insert();

-------------------------------------------------------------------------------
-- 5. Meta cache
-------------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS netlog_meta_cache (
    column_name  TEXT NOT NULL,
    value        TEXT NOT NULL,
    last_seen_at TIMESTAMPTZ,
    PRIMARY KEY (column_name, value)
);

-- ON CONFLICT DO NOTHING: once a value exists, skip with no lock/WAL/dead tuple.
CREATE OR REPLACE FUNCTION cache_netlog_meta()
RETURNS trigger AS $$
BEGIN
    INSERT INTO netlog_meta_cache (column_name, value)
    VALUES
        ('hostname', NEW.hostname),
        ('programname', NEW.programname),
        ('syslogtag', NEW.syslogtag)
    ON CONFLICT DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_netlog_meta_cache ON netlog_events;
CREATE TRIGGER trg_netlog_meta_cache
    AFTER INSERT ON netlog_events
    FOR EACH ROW EXECUTE FUNCTION cache_netlog_meta();

-------------------------------------------------------------------------------
-- 6. Facility cache
-------------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS netlog_facility_cache (
    facility SMALLINT PRIMARY KEY
);

CREATE OR REPLACE FUNCTION cache_netlog_facility()
RETURNS trigger AS $$
BEGIN
    INSERT INTO netlog_facility_cache (facility)
    VALUES (NEW.facility)
    ON CONFLICT DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_netlog_facility_cache ON netlog_events;
CREATE TRIGGER trg_netlog_facility_cache
    AFTER INSERT ON netlog_events
    FOR EACH ROW EXECUTE FUNCTION cache_netlog_facility();

-------------------------------------------------------------------------------
-- 7. Msg pattern trigger
-------------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION compute_netlog_msg_pattern()
RETURNS trigger AS $$
BEGIN
    NEW.msg_pattern := regexp_replace(
        regexp_replace(left(NEW.message, 200), '\d{1,3}(\.\d{1,3}){3}(:\d+)?', '<ip>', 'g'),
        '\d+', '<n>', 'g'
    );
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_netlog_msg_pattern ON netlog_events;
CREATE TRIGGER trg_netlog_msg_pattern
    BEFORE INSERT ON netlog_events
    FOR EACH ROW EXECUTE FUNCTION compute_netlog_msg_pattern();

-------------------------------------------------------------------------------
-- 8. Columnstore policy
-------------------------------------------------------------------------------

CALL add_columnstore_policy('netlog_events', after => INTERVAL '1 day', if_not_exists => true);

-------------------------------------------------------------------------------
-- 9. Retention policy
-------------------------------------------------------------------------------

SELECT add_retention_policy('netlog_events', INTERVAL '90 days', if_not_exists => true);

-------------------------------------------------------------------------------
-- 10. Continuous aggregate
-------------------------------------------------------------------------------

CREATE MATERIALIZED VIEW IF NOT EXISTS netlog_summary_hourly
WITH (timescaledb.continuous) AS
SELECT
    time_bucket('1 hour', received_at) AS bucket,
    hostname,
    severity,
    count(*) AS cnt
FROM netlog_events
GROUP BY bucket, hostname, severity
WITH NO DATA;

SELECT add_continuous_aggregate_policy('netlog_summary_hourly',
    start_offset    => INTERVAL '3 hours',
    end_offset      => INTERVAL '1 hour',
    schedule_interval => INTERVAL '1 hour',
    if_not_exists   => true
);

-------------------------------------------------------------------------------
-- 11. Real-time aggregation
-------------------------------------------------------------------------------

ALTER MATERIALIZED VIEW netlog_summary_hourly SET (timescaledb.materialized_only = false);

-------------------------------------------------------------------------------
-- 12. Extend notification_rules CHECK to include 'netlog'
-------------------------------------------------------------------------------

-- Dynamically find and replace the event_kind CHECK constraint.
DO $$
DECLARE
    _conname TEXT;
BEGIN
    SELECT conname INTO _conname
    FROM pg_constraint
    WHERE conrelid = 'notification_rules'::regclass
      AND contype = 'c'
      AND pg_get_constraintdef(oid) ILIKE '%event_kind%';

    IF _conname IS NOT NULL THEN
        EXECUTE format('ALTER TABLE notification_rules DROP CONSTRAINT %I', _conname);
    END IF;

    ALTER TABLE notification_rules
        ADD CONSTRAINT notification_rules_event_kind_check
            CHECK (event_kind IN ('srvlog', 'applog', 'netlog'));
END;
$$;

-------------------------------------------------------------------------------
-- 13. Taillight metrics: add netlog columns
-------------------------------------------------------------------------------

ALTER TABLE taillight_metrics ADD COLUMN IF NOT EXISTS sse_clients_netlog      INTEGER NOT NULL DEFAULT 0;
ALTER TABLE taillight_metrics ADD COLUMN IF NOT EXISTS netlog_events_broadcast BIGINT  NOT NULL DEFAULT 0;
ALTER TABLE taillight_metrics ADD COLUMN IF NOT EXISTS netlog_events_dropped   BIGINT  NOT NULL DEFAULT 0;
