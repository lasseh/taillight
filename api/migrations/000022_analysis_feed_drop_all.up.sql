-- Remove the combined "all" analysis feed (ADR 0006). An analysis run now
-- targets exactly one events table; the srvlog+netlog UNION path is gone
-- from the analyzer, so rows carrying feed = 'all' can no longer be served.
-- Production never created any; local and demo databases may have.

DELETE FROM analysis_schedules WHERE feed = 'all';
DELETE FROM analysis_reports  WHERE feed = 'all';

-- Both CHECKs were declared inline in migration 13, so they carry the
-- Postgres auto-generated names <table>_<column>_check. No IF EXISTS on
-- purpose: a wrong name must fail here, not leave the old constraint behind.
ALTER TABLE analysis_reports
    DROP CONSTRAINT analysis_reports_feed_check,
    ADD CONSTRAINT analysis_reports_feed_check CHECK (feed IN ('netlog', 'srvlog'));

ALTER TABLE analysis_schedules
    DROP CONSTRAINT analysis_schedules_feed_check,
    ADD CONSTRAINT analysis_schedules_feed_check CHECK (feed IN ('netlog', 'srvlog'));
