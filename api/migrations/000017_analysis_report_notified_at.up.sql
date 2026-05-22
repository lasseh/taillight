-- Adds a CAS column the worker uses to fire the completion notification
-- exactly once per report. The worker calls MarkReportCompleted and then runs
-- an atomic UPDATE ... WHERE notified_at IS NULL — only the winning UPDATE
-- triggers an email so a retry on the same row can't deliver a duplicate.
ALTER TABLE analysis_reports ADD COLUMN notified_at TIMESTAMPTZ NULL;
