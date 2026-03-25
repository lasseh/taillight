-- Reverse 000010: re-drop meta cache triggers.
DROP TRIGGER IF EXISTS trg_syslog_meta_cache ON syslog_events;
DROP FUNCTION IF EXISTS cache_syslog_meta();

DROP TRIGGER IF EXISTS trg_applog_meta_cache ON applog_events;
DROP FUNCTION IF EXISTS cache_applog_meta();
