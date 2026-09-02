-- Re-admit 'all' on both tables. Rows deleted by the up migration are not
-- restored; the code that could serve them is gone.
ALTER TABLE analysis_reports
    DROP CONSTRAINT analysis_reports_feed_check,
    ADD CONSTRAINT analysis_reports_feed_check CHECK (feed IN ('netlog', 'srvlog', 'all'));

ALTER TABLE analysis_schedules
    DROP CONSTRAINT analysis_schedules_feed_check,
    ADD CONSTRAINT analysis_schedules_feed_check CHECK (feed IN ('netlog', 'srvlog', 'all'));
