-- The ingest NOTIFY payload carries the row's received_at next to its id, as
-- "<id> <received_at in unix microseconds>". The api's ingest bridge matches
-- the row on both, so its per-event fetch reads only the chunk that holds the
-- row instead of every retained chunk. A received_at that is not finite has
-- no epoch value; that row sends its id alone, the form the api also accepts
-- from before this migration, so the trigger never fails an insert.
--
-- An api older than this migration cannot read the new payload: upgrade the
-- api with or before it. The api container runs migrations on start, which
-- does both at once.

CREATE OR REPLACE FUNCTION notify_srvlog_insert()
RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('srvlog_ingest',
        CASE WHEN isfinite(NEW.received_at)
            THEN NEW.id::text || ' ' || (extract(epoch FROM NEW.received_at) * 1000000)::bigint::text
            ELSE NEW.id::text
        END);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION notify_netlog_insert()
RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('netlog_ingest',
        CASE WHEN isfinite(NEW.received_at)
            THEN NEW.id::text || ' ' || (extract(epoch FROM NEW.received_at) * 1000000)::bigint::text
            ELSE NEW.id::text
        END);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
