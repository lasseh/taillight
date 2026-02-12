-- Taillight init schema.
-- Requires: TimescaleDB, pg_trgm, pg_stat_statements.

-------------------------------------------------------------------------------
-- 1. Extensions
-------------------------------------------------------------------------------

CREATE EXTENSION IF NOT EXISTS timescaledb;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;

-------------------------------------------------------------------------------
-- 2. Syslog events hypertable
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

-- Notify trigger for LISTEN/NOTIFY push to SSE broker.
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

-- Compression: convert chunks older than 1 day to columnstore.
CALL remove_columnstore_policy('syslog_events');
CALL add_columnstore_policy('syslog_events', after => INTERVAL '1 day');

-- Retention policy: automatically drop chunks older than 90 days.
SELECT add_retention_policy('syslog_events', INTERVAL '90 days', if_not_exists => true);

-- Tune autovacuum for high-insert workload.
ALTER TABLE syslog_events SET (
    autovacuum_vacuum_scale_factor = 0.05,
    autovacuum_analyze_scale_factor = 0.02
);

-------------------------------------------------------------------------------
-- 3. Juniper syslog reference table
-------------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS juniper_syslog_ref (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name        TEXT NOT NULL,
    message     TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    type        TEXT NOT NULL DEFAULT '',
    severity    TEXT NOT NULL DEFAULT '',
    cause       TEXT NOT NULL DEFAULT '',
    action      TEXT NOT NULL DEFAULT '',
    os          TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_juniper_ref_name_os ON juniper_syslog_ref (name, os);
CREATE INDEX IF NOT EXISTS idx_juniper_ref_name ON juniper_syslog_ref (name);

-------------------------------------------------------------------------------
-- 4. Application log events hypertable
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

-- Cursor pagination (keyset: received_at DESC, id DESC)
CREATE INDEX IF NOT EXISTS idx_applog_received_id ON applog_events (received_at DESC, id DESC);
-- Filter indexes
CREATE INDEX IF NOT EXISTS idx_applog_service_received ON applog_events (service, received_at DESC);
CREATE INDEX IF NOT EXISTS idx_applog_level_received ON applog_events (level, received_at DESC);
-- Full-text search
CREATE INDEX IF NOT EXISTS idx_applog_search ON applog_events USING GIN (search_vector);

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
-- 5. Meta cache tables + triggers
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

-- Applog meta cache.
CREATE TABLE IF NOT EXISTS applog_meta_cache (
    column_name TEXT NOT NULL,
    value       TEXT NOT NULL,
    PRIMARY KEY (column_name, value)
);

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
-- 6. Authentication: users, sessions, API keys
-------------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username      TEXT NOT NULL CHECK (length(username) BETWEEN 1 AND 255),
    password_hash TEXT NOT NULL,
    email         TEXT,
    is_active     BOOLEAN NOT NULL DEFAULT true,
    is_admin      BOOLEAN NOT NULL DEFAULT false,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username ON users (LOWER(username));

CREATE TABLE IF NOT EXISTS sessions (
    token_hash   TEXT PRIMARY KEY,
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ NOT NULL,
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ip_address   INET,
    user_agent   TEXT NOT NULL DEFAULT ''
);
CREATE INDEX idx_sessions_expires ON sessions (expires_at);
CREATE INDEX idx_sessions_user ON sessions (user_id);

CREATE TABLE IF NOT EXISTS api_keys (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 255),
    key_hash     TEXT NOT NULL,
    key_prefix   TEXT NOT NULL DEFAULT '',
    scopes       TEXT[] NOT NULL DEFAULT '{}',
    expires_at   TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_api_keys_hash ON api_keys (key_hash) WHERE revoked_at IS NULL;
CREATE INDEX idx_api_keys_user ON api_keys (user_id);

-------------------------------------------------------------------------------
-- 7. Analysis reports
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
-- 8. Notifications
-------------------------------------------------------------------------------

-- Notification channels (configured backends).
CREATE TABLE notification_channels (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       TEXT UNIQUE NOT NULL,
    type       TEXT NOT NULL CHECK (type IN ('slack','webhook')),
    config     JSONB NOT NULL DEFAULT '{}',
    enabled    BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Notification rules (alert conditions).
CREATE TABLE notification_rules (
    id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name             TEXT UNIQUE NOT NULL,
    enabled          BOOLEAN NOT NULL DEFAULT true,
    event_kind       TEXT NOT NULL CHECK (event_kind IN ('syslog', 'applog')),
    -- Syslog filter fields (nullable = don't filter on this field).
    hostname         TEXT,
    programname      TEXT,
    severity         SMALLINT CHECK (severity BETWEEN 0 AND 7),
    severity_max     SMALLINT CHECK (severity_max BETWEEN 0 AND 7),
    facility         SMALLINT CHECK (facility BETWEEN 0 AND 23),
    syslogtag        TEXT,
    msgid            TEXT,
    -- AppLog filter fields.
    service          TEXT,
    component        TEXT,
    host             TEXT,
    level            TEXT CHECK (level IN ('DEBUG','INFO','WARN','ERROR','FATAL')),
    -- Shared.
    search           TEXT,
    -- Behavior.
    burst_window     INTEGER NOT NULL DEFAULT 30,
    cooldown_seconds INTEGER NOT NULL DEFAULT 300,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Many-to-many: rules → channels.
CREATE TABLE notification_rule_channels (
    rule_id    BIGINT REFERENCES notification_rules(id) ON DELETE CASCADE,
    channel_id BIGINT REFERENCES notification_channels(id) ON DELETE CASCADE,
    PRIMARY KEY (rule_id, channel_id)
);

-- Audit trail (hypertable with columnstore).
CREATE TABLE notification_log (
    id          BIGINT GENERATED ALWAYS AS IDENTITY,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    rule_id     BIGINT NOT NULL,
    channel_id  BIGINT NOT NULL,
    event_kind  TEXT NOT NULL,
    event_id    BIGINT NOT NULL,
    status      TEXT NOT NULL CHECK (status IN ('sent','suppressed','failed')),
    reason      TEXT,
    event_count INT NOT NULL DEFAULT 1,
    status_code INTEGER,
    duration_ms INTEGER NOT NULL DEFAULT 0,
    payload     JSONB
) WITH (
    tsdb.hypertable,
    tsdb.partition_column = 'created_at',
    tsdb.chunk_interval = '7 days',
    tsdb.columnstore = true,
    tsdb.orderby = 'created_at DESC'
);

SELECT add_retention_policy('notification_log', INTERVAL '30 days', if_not_exists => true);

CREATE INDEX idx_notification_log_rule ON notification_log (rule_id, created_at DESC);
CREATE INDEX idx_notification_log_status ON notification_log (status, created_at DESC);

-------------------------------------------------------------------------------
-- 9. rsyslog impstats telemetry
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
-- 10. Taillight application metrics
-------------------------------------------------------------------------------

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
