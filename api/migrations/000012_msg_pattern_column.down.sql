-- Reverse 000012: remove msg_pattern columns and triggers.
DROP TRIGGER IF EXISTS trg_syslog_msg_pattern ON syslog_events;
DROP FUNCTION IF EXISTS compute_syslog_msg_pattern();
ALTER TABLE syslog_events DROP COLUMN IF EXISTS msg_pattern;

DROP TRIGGER IF EXISTS trg_applog_msg_pattern ON applog_events;
DROP FUNCTION IF EXISTS compute_applog_msg_pattern();
ALTER TABLE applog_events DROP COLUMN IF EXISTS msg_pattern;
