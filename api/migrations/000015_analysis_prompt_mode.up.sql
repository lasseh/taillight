-------------------------------------------------------------------------------
-- Per-mode analysis prompts.
--
-- Adds a prompt_mode column to analysis_reports so each row records which
-- prompt set produced it (daily / weekly / incident). Extends the partial
-- unique active-report index to include prompt_mode so two modes can run
-- concurrently for the same (feed, period_end) — e.g. a scheduled daily
-- brief and a manual incident triage in the same minute won't collide.
--
-- Existing rows backfill to 'daily' (the only mode that existed before
-- this change). Slugs of existing rows are left untouched; new rows will
-- include a mode segment via store-side BuildAnalysisSlug.
-------------------------------------------------------------------------------

ALTER TABLE analysis_reports
    ADD COLUMN prompt_mode TEXT NOT NULL DEFAULT 'daily';

-- Replace the partial unique index with one that includes prompt_mode so
-- different modes can have concurrent pending/running rows for the same
-- feed+window. Two reports in the same mode + feed + window still collide
-- and surface ErrDuplicateActiveReport, which is the intended behaviour.
DROP INDEX IF EXISTS analysis_reports_active_uniq;

CREATE UNIQUE INDEX analysis_reports_active_uniq
    ON analysis_reports (feed, period_end, prompt_mode)
    WHERE status IN ('pending', 'running');
