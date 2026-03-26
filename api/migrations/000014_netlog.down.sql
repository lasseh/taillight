-- Reverse 000014_netlog.up.sql

-- Remove netlog columns from taillight_metrics.
ALTER TABLE taillight_metrics DROP COLUMN IF EXISTS netlog_events_dropped;
ALTER TABLE taillight_metrics DROP COLUMN IF EXISTS netlog_events_broadcast;
ALTER TABLE taillight_metrics DROP COLUMN IF EXISTS sse_clients_netlog;

-- Restore notification_rules CHECK to exclude 'netlog'.
DO $$
DECLARE
    _conname TEXT;
BEGIN
    SELECT conname INTO _conname
    FROM pg_constraint
    WHERE conrelid = 'notification_rules'::regclass
      AND contype = 'c'
      AND pg_get_constraintdef(oid) ILIKE '%event_kind%';

    IF _conname IS NOT NULL THEN
        EXECUTE format('ALTER TABLE notification_rules DROP CONSTRAINT %I', _conname);
    END IF;

    ALTER TABLE notification_rules
        ADD CONSTRAINT notification_rules_event_kind_check
            CHECK (event_kind IN ('srvlog', 'applog'));
END;
$$;

-- Drop continuous aggregate + policy.
SELECT remove_continuous_aggregate_policy('netlog_summary_hourly', if_exists => true);
DROP MATERIALIZED VIEW IF EXISTS netlog_summary_hourly;

-- Remove policies.
SELECT remove_retention_policy('netlog_events', if_exists => true);

-- Drop triggers.
DROP TRIGGER IF EXISTS trg_netlog_msg_pattern ON netlog_events;
DROP TRIGGER IF EXISTS trg_netlog_facility_cache ON netlog_events;
DROP TRIGGER IF EXISTS trg_netlog_meta_cache ON netlog_events;
DROP TRIGGER IF EXISTS trg_netlog_notify ON netlog_events;

-- Drop trigger functions.
DROP FUNCTION IF EXISTS compute_netlog_msg_pattern();
DROP FUNCTION IF EXISTS cache_netlog_facility();
DROP FUNCTION IF EXISTS cache_netlog_meta();
DROP FUNCTION IF EXISTS notify_netlog_insert();

-- Drop cache tables.
DROP TABLE IF EXISTS netlog_facility_cache;
DROP TABLE IF EXISTS netlog_meta_cache;

-- Drop hypertable (cascades indexes).
DROP TABLE IF EXISTS netlog_events;
