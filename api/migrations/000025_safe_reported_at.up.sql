-- safe_reported_at: the rsyslog insert templates (PgSQLSrvlogInsert,
-- PgSQLNetlogInsert) pass the sender's timestamp through this function instead
-- of as a bare literal. A timestamp Postgres cannot store as timestamptz (an
-- impossible calendar date, year 0, a UTC offset of 16 hours or more) would
-- otherwise fail the INSERT and the whole ompgsql batch with it, and the
-- action retries that batch indefinitely, so later rows on the feed wait
-- behind it. The event is stored with now() as reported_at instead.
--
-- rsyslog must not use those templates before this migration has run; the
-- compose stack starts rsyslog only once the api, which migrates first, is
-- healthy.

CREATE OR REPLACE FUNCTION safe_reported_at(ts text)
RETURNS timestamptz AS $$
BEGIN
    RETURN ts::timestamptz;
EXCEPTION WHEN data_exception THEN
    RETURN now();
END;
$$ LANGUAGE plpgsql STABLE;
