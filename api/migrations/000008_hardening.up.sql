-------------------------------------------------------------------------------
-- M5. Index on notification_rules for rule lookup by event_kind + enabled.
-------------------------------------------------------------------------------

CREATE INDEX IF NOT EXISTS idx_notif_rules_event_kind
    ON notification_rules (event_kind, enabled);

-- H6 (generated column on rsyslog_stats) was dropped: TimescaleDB columnstore
-- does not support GENERATED ALWAYS AS columns. The Go code continues to use
-- the (stats ->> 'msg')::jsonb expression inline, which is acceptable given
-- that columnstore compression already optimizes read performance.

-------------------------------------------------------------------------------
-- C2. Remove meta cache INSERT triggers.
-- The syslog_meta_cache and applog_meta_cache tables remain for reads but
-- are no longer updated live on every INSERT. A periodic refresh in the
-- application layer is sufficient for the autocomplete use case.
-------------------------------------------------------------------------------

DROP TRIGGER IF EXISTS trg_syslog_meta_cache ON syslog_events;
DROP FUNCTION IF EXISTS cache_syslog_meta();

DROP TRIGGER IF EXISTS trg_applog_meta_cache ON applog_events;
DROP FUNCTION IF EXISTS cache_applog_meta();
