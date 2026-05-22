-------------------------------------------------------------------------------
-- Per-report host scope.
--
-- Adds a hosts column to analysis_reports so manual runs (and, eventually,
-- scheduled runs) can be restricted to an explicit set of hostnames. The
-- canonical "all hosts" value is the empty array — never NULL — so the
-- partial unique index below behaves consistently regardless of how Postgres
-- treats NULLs in array unique constraints.
--
-- Hosts are normalized (sorted, deduped) by the application before insert so
-- ["a","b"] and ["b","a","a"] collide on the active-report constraint as
-- intended.
--
-- The partial unique index gains hosts as its trailing key so two requests
-- with different host scopes for the same (feed, period, mode) can run
-- concurrently; two requests with the same scope still collide and surface
-- ErrDuplicateActiveReport.
-------------------------------------------------------------------------------

ALTER TABLE analysis_reports
    ADD COLUMN hosts TEXT[] NOT NULL DEFAULT '{}';

DROP INDEX IF EXISTS analysis_reports_active_uniq;

CREATE UNIQUE INDEX analysis_reports_active_uniq
    ON analysis_reports (feed, period_end, prompt_mode, hosts)
    WHERE status IN ('pending', 'running');
