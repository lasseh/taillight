-- Analysis reports v2: async lifecycle (pending/running/completed/failed),
-- per-feed reports, slug-based addressing, and a partial unique index to
-- enforce one active report per (feed, period_end).

ALTER TABLE analysis_reports
    -- Feed defaults to 'netlog' for any historical rows. This is correct for
    -- empty deployments; on a populated DB the default may mis-tag rows that
    -- were generated under a different analysis.feed config. Safe here
    -- because no historical rows exist.
    ADD COLUMN feed         TEXT NOT NULL DEFAULT 'netlog'
        CHECK (feed IN ('netlog', 'srvlog', 'all')),
    ADD COLUMN slug         TEXT,
    ADD COLUMN error        TEXT,
    ADD COLUMN created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN started_at   TIMESTAMPTZ,
    ADD COLUMN completed_at TIMESTAMPTZ;

-- Backfill timestamps from the legacy generated_at column. Every existing row
-- predates the async lifecycle and is effectively "already completed", so both
-- created_at and completed_at map to generated_at.
UPDATE analysis_reports
SET created_at   = generated_at,
    completed_at = generated_at;

-- Backfill slug for historical rows: <feed>-<YYYY-MM-DD>-<HHMM>.
UPDATE analysis_reports
SET slug = feed
        || '-' || to_char(period_end AT TIME ZONE 'UTC', 'YYYY-MM-DD')
        || '-' || to_char(period_end AT TIME ZONE 'UTC', 'HH24MI')
WHERE slug IS NULL;

ALTER TABLE analysis_reports
    ALTER COLUMN slug   SET NOT NULL,
    ALTER COLUMN report DROP NOT NULL,
    ADD CONSTRAINT analysis_reports_slug_uniq UNIQUE (slug),
    DROP COLUMN duration_ms,
    DROP COLUMN generated_at;

-- One active report per feed+period window. Enforces the duplicate-active rule
-- so two concurrent triggers (or a manual + schedule fire) can't both insert.
CREATE UNIQUE INDEX analysis_reports_active_uniq
    ON analysis_reports (feed, period_end)
    WHERE status IN ('pending', 'running');

CREATE INDEX IF NOT EXISTS idx_analysis_reports_created
    ON analysis_reports (created_at DESC);

DROP INDEX IF EXISTS idx_analysis_reports_generated;

-------------------------------------------------------------------------------
-- Analysis schedules: cron-like recurring runs, mirrors summary_schedules.
-------------------------------------------------------------------------------

CREATE TABLE analysis_schedules (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name         TEXT NOT NULL UNIQUE,
    enabled      BOOLEAN NOT NULL DEFAULT true,
    feed         TEXT NOT NULL CHECK (feed IN ('netlog', 'srvlog', 'all')),
    frequency    TEXT NOT NULL CHECK (frequency IN ('daily', 'weekly', 'monthly')),
    day_of_week  INT CHECK (day_of_week BETWEEN 0 AND 6),
    day_of_month INT CHECK (day_of_month BETWEEN 1 AND 28),
    time_of_day  TIME NOT NULL DEFAULT '03:00',
    timezone     TEXT NOT NULL DEFAULT 'UTC',
    last_run_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
