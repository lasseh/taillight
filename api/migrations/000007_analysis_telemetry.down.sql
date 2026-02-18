-- Reverse analysis and telemetry migration.

-- Taillight metrics.
SELECT remove_retention_policy('taillight_metrics', if_exists => true);
DROP TABLE IF EXISTS taillight_metrics;

-- rsyslog stats.
SELECT remove_retention_policy('rsyslog_stats', if_exists => true);
DROP TABLE IF EXISTS rsyslog_stats;

-- Analysis reports.
DROP TABLE IF EXISTS analysis_reports;
