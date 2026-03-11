-- Syslog events hypertable, indexes, triggers, and meta caches.

-------------------------------------------------------------------------------
-- 1. Hypertable
-------------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS syslog_events (
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
    raw_message     TEXT
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
CREATE INDEX IF NOT EXISTS idx_syslog_received_id
    ON syslog_events (received_at DESC, id DESC);

-- Single event lookup by ID (scans all chunks, acceptable for rare lookups).
CREATE INDEX IF NOT EXISTS idx_syslog_id
    ON syslog_events (id);

-- Filter indexes (compound with received_at for sort elimination).
CREATE INDEX IF NOT EXISTS idx_syslog_host_received
    ON syslog_events (hostname, received_at DESC);

CREATE INDEX IF NOT EXISTS idx_syslog_severity_received
    ON syslog_events (severity, received_at DESC, id DESC)
    WHERE severity <= 3;

CREATE INDEX IF NOT EXISTS idx_syslog_programname
    ON syslog_events (programname, received_at DESC);

CREATE INDEX IF NOT EXISTS idx_syslog_facility
    ON syslog_events (facility, received_at DESC);

CREATE INDEX IF NOT EXISTS idx_syslog_fromhost_ip
    ON syslog_events (fromhost_ip, received_at DESC);

CREATE INDEX IF NOT EXISTS idx_syslog_syslogtag
    ON syslog_events (syslogtag, received_at DESC);

-- Trigram index for ILIKE substring search on message.
CREATE INDEX IF NOT EXISTS idx_syslog_message_trgm
    ON syslog_events USING GIN (message gin_trgm_ops);

-------------------------------------------------------------------------------
-- 3. LISTEN/NOTIFY trigger
-------------------------------------------------------------------------------

-- Sends the row ID so the Go backend can fetch the full event by ID.
CREATE OR REPLACE FUNCTION notify_syslog_insert()
RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('syslog_ingest', NEW.id::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_syslog_notify ON syslog_events;
CREATE TRIGGER trg_syslog_notify
    AFTER INSERT ON syslog_events
    FOR EACH ROW EXECUTE FUNCTION notify_syslog_insert();

-------------------------------------------------------------------------------
-- 4. Columnstore + retention policies
-------------------------------------------------------------------------------

CALL remove_columnstore_policy('syslog_events');
CALL add_columnstore_policy('syslog_events', after => INTERVAL '1 day');

SELECT add_retention_policy('syslog_events', INTERVAL '90 days', if_not_exists => true);

-- Tune autovacuum for high-insert workload.
ALTER TABLE syslog_events SET (
    autovacuum_vacuum_scale_factor = 0.05,
    autovacuum_analyze_scale_factor = 0.02
);

-------------------------------------------------------------------------------
-- 5. Meta caches
-------------------------------------------------------------------------------

-- Syslog meta cache (last_seen_at tracks hostname freshness).
CREATE TABLE IF NOT EXISTS syslog_meta_cache (
    column_name  TEXT NOT NULL,
    value        TEXT NOT NULL,
    last_seen_at TIMESTAMPTZ,
    PRIMARY KEY (column_name, value)
);

CREATE OR REPLACE FUNCTION cache_syslog_meta()
RETURNS trigger AS $$
BEGIN
    INSERT INTO syslog_meta_cache (column_name, value, last_seen_at)
    VALUES
        ('hostname', NEW.hostname, now()),
        ('programname', NEW.programname, NULL),
        ('syslogtag', NEW.syslogtag, NULL)
    ON CONFLICT (column_name, value) DO UPDATE
        SET last_seen_at = CASE
            WHEN EXCLUDED.column_name = 'hostname' THEN now()
            ELSE syslog_meta_cache.last_seen_at
        END;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_syslog_meta_cache ON syslog_events;
CREATE TRIGGER trg_syslog_meta_cache
    AFTER INSERT ON syslog_events
    FOR EACH ROW EXECUTE FUNCTION cache_syslog_meta();

-- Syslog facility cache.
CREATE TABLE IF NOT EXISTS syslog_facility_cache (
    facility SMALLINT PRIMARY KEY
);

CREATE OR REPLACE FUNCTION cache_syslog_facility()
RETURNS trigger AS $$
BEGIN
    INSERT INTO syslog_facility_cache (facility)
    VALUES (NEW.facility)
    ON CONFLICT DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_syslog_facility_cache ON syslog_events;
CREATE TRIGGER trg_syslog_facility_cache
    AFTER INSERT ON syslog_events
    FOR EACH ROW EXECUTE FUNCTION cache_syslog_facility();
