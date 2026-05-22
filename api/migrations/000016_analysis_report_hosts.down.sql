DROP INDEX IF EXISTS analysis_reports_active_uniq;

CREATE UNIQUE INDEX analysis_reports_active_uniq
    ON analysis_reports (feed, period_end, prompt_mode)
    WHERE status IN ('pending', 'running');

ALTER TABLE analysis_reports
    DROP COLUMN IF EXISTS hosts;
