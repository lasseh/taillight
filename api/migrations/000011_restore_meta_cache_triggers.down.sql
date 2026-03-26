-- Reverse 000011: re-drop meta cache triggers.
DROP TRIGGER IF EXISTS trg_srvlog_meta_cache ON srvlog_events;
DROP FUNCTION IF EXISTS cache_srvlog_meta();

DROP TRIGGER IF EXISTS trg_applog_meta_cache ON applog_events;
DROP FUNCTION IF EXISTS cache_applog_meta();
