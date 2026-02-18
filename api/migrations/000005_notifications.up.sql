-- Notification channels, rules, and audit log.

-------------------------------------------------------------------------------
-- 1. Channels (configured backends)
-------------------------------------------------------------------------------

CREATE TABLE notification_channels (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       TEXT UNIQUE NOT NULL,
    type       TEXT NOT NULL CHECK (type IN ('slack', 'webhook', 'email')),
    config     JSONB NOT NULL DEFAULT '{}',
    enabled    BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-------------------------------------------------------------------------------
-- 2. Rules (alert conditions)
-------------------------------------------------------------------------------

CREATE TABLE notification_rules (
    id                  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name                TEXT UNIQUE NOT NULL,
    enabled             BOOLEAN NOT NULL DEFAULT true,
    event_kind          TEXT NOT NULL CHECK (event_kind IN ('syslog', 'applog')),
    -- Syslog filter fields (nullable = don't filter on this field).
    hostname            TEXT,
    programname         TEXT,
    severity            SMALLINT CHECK (severity BETWEEN 0 AND 7),
    severity_max        SMALLINT CHECK (severity_max BETWEEN 0 AND 7),
    facility            SMALLINT CHECK (facility BETWEEN 0 AND 23),
    syslogtag           TEXT,
    msgid               TEXT,
    -- AppLog filter fields.
    service             TEXT,
    component           TEXT,
    host                TEXT,
    level               TEXT CHECK (level IN ('DEBUG','INFO','WARN','ERROR','FATAL')),
    -- Shared.
    search              TEXT,
    -- Behavior.
    burst_window        INTEGER NOT NULL DEFAULT 30,
    cooldown_seconds    INTEGER NOT NULL DEFAULT 60,
    group_by            TEXT NOT NULL DEFAULT 'hostname',
    max_cooldown_seconds INTEGER NOT NULL DEFAULT 3600,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

-------------------------------------------------------------------------------
-- 3. Many-to-many: rules → channels
-------------------------------------------------------------------------------

CREATE TABLE notification_rule_channels (
    rule_id    BIGINT REFERENCES notification_rules(id) ON DELETE CASCADE,
    channel_id BIGINT REFERENCES notification_channels(id) ON DELETE CASCADE,
    PRIMARY KEY (rule_id, channel_id)
);

-------------------------------------------------------------------------------
-- 4. Audit trail (hypertable with columnstore)
-------------------------------------------------------------------------------

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
