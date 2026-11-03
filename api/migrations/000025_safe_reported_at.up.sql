-- safe_reported_at: the rsyslog insert templates (PgSQLSrvlogInsert,
-- PgSQLNetlogInsert) pass the sender's timestamp through this function instead
-- of as a bare literal. A timestamp Postgres cannot store as timestamptz (an
-- impossible calendar date, year 0, a UTC offset of 16 hours or more) would
-- otherwise fail the INSERT and the whole ompgsql batch with it, and the
-- action retries that batch indefinitely, so later rows on the feed wait
-- behind it. The event is stored with now() as reported_at instead.
--
-- A timestamp outside UTC years 1 to 9998 also gets now(), the bound applog
-- ingest applies: the API's JSON encoding handles years 0 to 9999 only, and
-- the year it sees depends on the server's time zone, so such a row would
-- fail every list response that includes it.
--
-- rsyslog must not use those templates before this migration has run; the
-- compose stack starts rsyslog only once the api, which migrates first, is
-- healthy.

CREATE OR REPLACE FUNCTION safe_reported_at(ts text)
RETURNS timestamptz AS $$
DECLARE
    v timestamptz;
BEGIN
    v := ts::timestamptz;
    IF v < '0001-01-01 00:00:00+00'::timestamptz
        OR v >= '9999-01-01 00:00:00+00'::timestamptz THEN
        RETURN now();
    END IF;
    RETURN v;
EXCEPTION WHEN data_exception THEN
    RETURN now();
END;
$$ LANGUAGE plpgsql STABLE;
