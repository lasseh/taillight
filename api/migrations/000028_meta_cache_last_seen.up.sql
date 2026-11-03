-- Meta cache values record when they were last logged, so the api can prune
-- values with no event inside the feed's retention.
--
-- Rows cached before this migration have no last_seen_at. They start the
-- clock now and age out one retention period from today unless they log
-- again; finding their real last event would read every retained chunk.
--
-- The triggers write last_seen_at from the event's received_at, at most once
-- an hour per value, so a busy value costs one row update an hour rather than
-- one per event. The prune in the api allows for that hour of lag.
--
-- Neither statement in the triggers waits on a row lock: the refresh skips
-- rows another transaction holds (it is refreshing or pruning them), and the
-- insert only reaches the unique index for values not cached yet. rsyslog
-- writes each feed from several workers in batches, and batches that waited
-- on each other's rows could deadlock.

UPDATE srvlog_meta_cache SET last_seen_at = now() WHERE last_seen_at IS NULL;
UPDATE netlog_meta_cache SET last_seen_at = now() WHERE last_seen_at IS NULL;
UPDATE applog_meta_cache SET last_seen_at = now() WHERE last_seen_at IS NULL;

ALTER TABLE srvlog_meta_cache ALTER COLUMN last_seen_at SET DEFAULT now();
ALTER TABLE netlog_meta_cache ALTER COLUMN last_seen_at SET DEFAULT now();
ALTER TABLE applog_meta_cache ALTER COLUMN last_seen_at SET DEFAULT now();

CREATE OR REPLACE FUNCTION cache_srvlog_meta()
RETURNS trigger AS $$
BEGIN
    UPDATE srvlog_meta_cache SET last_seen_at = NEW.received_at
    WHERE ctid = ANY (ARRAY(
        SELECT ctid FROM srvlog_meta_cache
        WHERE (column_name, value) IN (('hostname', NEW.hostname),
                                       ('programname', NEW.programname),
                                       ('syslogtag', NEW.syslogtag))
          AND last_seen_at < NEW.received_at - INTERVAL '1 hour'
        FOR UPDATE SKIP LOCKED));

    INSERT INTO srvlog_meta_cache (column_name, value, last_seen_at)
    SELECT v.column_name, v.value, NEW.received_at
    FROM (VALUES ('hostname', NEW.hostname),
                 ('programname', NEW.programname),
                 ('syslogtag', NEW.syslogtag)) AS v(column_name, value)
    WHERE NOT EXISTS (SELECT 1 FROM srvlog_meta_cache c
                      WHERE c.column_name = v.column_name AND c.value = v.value)
    ON CONFLICT DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION cache_netlog_meta()
RETURNS trigger AS $$
BEGIN
    UPDATE netlog_meta_cache SET last_seen_at = NEW.received_at
    WHERE ctid = ANY (ARRAY(
        SELECT ctid FROM netlog_meta_cache
        WHERE (column_name, value) IN (('hostname', NEW.hostname),
                                       ('programname', NEW.programname),
                                       ('syslogtag', NEW.syslogtag))
          AND last_seen_at < NEW.received_at - INTERVAL '1 hour'
        FOR UPDATE SKIP LOCKED));

    INSERT INTO netlog_meta_cache (column_name, value, last_seen_at)
    SELECT v.column_name, v.value, NEW.received_at
    FROM (VALUES ('hostname', NEW.hostname),
                 ('programname', NEW.programname),
                 ('syslogtag', NEW.syslogtag)) AS v(column_name, value)
    WHERE NOT EXISTS (SELECT 1 FROM netlog_meta_cache c
                      WHERE c.column_name = v.column_name AND c.value = v.value)
    ON CONFLICT DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION cache_applog_meta()
RETURNS trigger AS $$
BEGIN
    UPDATE applog_meta_cache SET last_seen_at = NEW.received_at
    WHERE ctid = ANY (ARRAY(
        SELECT ctid FROM applog_meta_cache
        WHERE (column_name, value) IN (('service', NEW.service),
                                       ('component', NEW.component),
                                       ('host', NEW.host))
          AND last_seen_at < NEW.received_at - INTERVAL '1 hour'
        FOR UPDATE SKIP LOCKED));

    INSERT INTO applog_meta_cache (column_name, value, last_seen_at)
    SELECT v.column_name, v.value, NEW.received_at
    FROM (VALUES ('service', NEW.service),
                 ('component', NEW.component),
                 ('host', NEW.host)) AS v(column_name, value)
    WHERE NOT EXISTS (SELECT 1 FROM applog_meta_cache c
                      WHERE c.column_name = v.column_name AND c.value = v.value)
    ON CONFLICT DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
