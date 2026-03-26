-- Reverse 000012: remove msg_pattern columns and triggers.
DROP TRIGGER IF EXISTS trg_srvlog_msg_pattern ON srvlog_events;
DROP FUNCTION IF EXISTS compute_srvlog_msg_pattern();
ALTER TABLE srvlog_events DROP COLUMN IF EXISTS msg_pattern;

DROP TRIGGER IF EXISTS trg_applog_msg_pattern ON applog_events;
DROP FUNCTION IF EXISTS compute_applog_msg_pattern();
ALTER TABLE applog_events DROP COLUMN IF EXISTS msg_pattern;
