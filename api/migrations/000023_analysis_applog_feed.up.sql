-- Applog joins the analysis feeds (ADR 0006).
--
-- Reports gain a services column, the applog counterpart of hosts: the
-- canonical "all services" value is the empty array, never NULL, and the
-- application normalizes (sorts, dedupes) the list before insert. The
-- partial unique index takes services as a trailing key so two applog runs
-- with different service scopes for the same window can run concurrently.
-- Both feed CHECKs admit 'applog'; migration 22 named them explicitly.

ALTER TABLE analysis_reports
    ADD COLUMN services TEXT[] NOT NULL DEFAULT '{}';

DROP INDEX IF EXISTS analysis_reports_active_uniq;

CREATE UNIQUE INDEX analysis_reports_active_uniq
    ON analysis_reports (feed, period_end, prompt_mode, hosts, services)
    WHERE status IN ('pending', 'running');

ALTER TABLE analysis_reports
    DROP CONSTRAINT analysis_reports_feed_check,
    ADD CONSTRAINT analysis_reports_feed_check CHECK (feed IN ('netlog', 'srvlog', 'applog'));

ALTER TABLE analysis_schedules
    DROP CONSTRAINT analysis_schedules_feed_check,
    ADD CONSTRAINT analysis_schedules_feed_check CHECK (feed IN ('netlog', 'srvlog', 'applog'));
