-- Restore the id-only NOTIFY payload. The api still accepts that form and
-- falls back to fetching the row by id alone.

CREATE OR REPLACE FUNCTION notify_srvlog_insert()
RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('srvlog_ingest', NEW.id::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION notify_netlog_insert()
RETURNS trigger AS $$
BEGIN
    PERFORM pg_notify('netlog_ingest', NEW.id::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
