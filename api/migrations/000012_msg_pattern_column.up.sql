-------------------------------------------------------------------------------
-- Add a pre-computed msg_pattern column to srvlog_events and applog_events.
--
-- The device summary "top messages" query previously ran regexp_replace on
-- every row at query time. By computing the pattern once on INSERT and
-- storing it, the query becomes a simple GROUP BY on a regular column.
--
-- TimescaleDB columnstore does not support GENERATED ALWAYS AS columns,
-- so we use a BEFORE INSERT trigger instead.
--
-- Existing rows are NOT backfilled — the column defaults to '' for old data.
-- Since top_messages uses a 24h window, the column is fully populated within
-- one day of deploying this migration.
-------------------------------------------------------------------------------

-- Srvlog: add column + trigger.
ALTER TABLE srvlog_events ADD COLUMN IF NOT EXISTS msg_pattern TEXT NOT NULL DEFAULT '';

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

-- Applog: add column + trigger.
ALTER TABLE applog_events ADD COLUMN IF NOT EXISTS msg_pattern TEXT NOT NULL DEFAULT '';

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
