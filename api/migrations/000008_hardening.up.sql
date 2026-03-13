-------------------------------------------------------------------------------
-- M5. Index on notification_rules for rule lookup by event_kind + enabled.
-------------------------------------------------------------------------------

CREATE INDEX IF NOT EXISTS idx_notif_rules_event_kind
    ON notification_rules (event_kind, enabled);

-------------------------------------------------------------------------------
-- H6. Generated column for rsyslog_stats inner JSON extraction.
-- ompgsql stores impstats as {"msg": "{ ... }"}, so every query must parse
-- the inner JSON string. A stored generated column materializes this once
-- on write and lets queries use a plain JSONB column reference.
-------------------------------------------------------------------------------

ALTER TABLE rsyslog_stats
    ADD COLUMN IF NOT EXISTS inner_stats JSONB
    GENERATED ALWAYS AS ((stats ->> 'msg')::jsonb) STORED;

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
