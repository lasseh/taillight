DROP TABLE IF EXISTS analysis_schedules;

DROP INDEX IF EXISTS analysis_reports_active_uniq;
DROP INDEX IF EXISTS idx_analysis_reports_created;

ALTER TABLE analysis_reports
    ADD COLUMN IF NOT EXISTS generated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ADD COLUMN IF NOT EXISTS duration_ms  BIGINT NOT NULL DEFAULT 0;

UPDATE analysis_reports
SET generated_at = COALESCE(completed_at, created_at);

-- Pre-v2 schema required report NOT NULL. Drop any rows that never had a body.
DELETE FROM analysis_reports WHERE report IS NULL;

ALTER TABLE analysis_reports
    ALTER COLUMN report SET NOT NULL,
    DROP CONSTRAINT IF EXISTS analysis_reports_slug_uniq,
    DROP COLUMN IF EXISTS feed,
    DROP COLUMN IF EXISTS slug,
    DROP COLUMN IF EXISTS error,
    DROP COLUMN IF EXISTS created_at,
    DROP COLUMN IF EXISTS started_at,
    DROP COLUMN IF EXISTS completed_at;

CREATE INDEX IF NOT EXISTS idx_analysis_reports_generated
    ON analysis_reports (generated_at DESC);
