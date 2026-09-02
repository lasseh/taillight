-- Remove the applog feed again. Applog rows cannot survive the narrower
-- CHECK, so they are deleted first.

DELETE FROM analysis_schedules WHERE feed = 'applog';
DELETE FROM analysis_reports  WHERE feed = 'applog';

ALTER TABLE analysis_reports
    DROP CONSTRAINT analysis_reports_feed_check,
    ADD CONSTRAINT analysis_reports_feed_check CHECK (feed IN ('netlog', 'srvlog'));

ALTER TABLE analysis_schedules
    DROP CONSTRAINT analysis_schedules_feed_check,
    ADD CONSTRAINT analysis_schedules_feed_check CHECK (feed IN ('netlog', 'srvlog'));

DROP INDEX IF EXISTS analysis_reports_active_uniq;

CREATE UNIQUE INDEX analysis_reports_active_uniq
    ON analysis_reports (feed, period_end, prompt_mode, hosts)
    WHERE status IN ('pending', 'running');

ALTER TABLE analysis_reports DROP COLUMN services;
