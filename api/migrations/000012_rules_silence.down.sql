-- Revert to the old burst/cooldown columns. Existing rule behaviour tuning
-- does NOT round-trip: rules are reset to the pre-migration defaults.

ALTER TABLE notification_rules
    ADD COLUMN burst_window         INTEGER NOT NULL DEFAULT 30,
    ADD COLUMN cooldown_seconds     INTEGER NOT NULL DEFAULT 60,
    ADD COLUMN max_cooldown_seconds INTEGER NOT NULL DEFAULT 3600;

ALTER TABLE notification_rules
    DROP COLUMN silence_ms,
    DROP COLUMN silence_max_ms,
    DROP COLUMN coalesce_ms;
