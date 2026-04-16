-- Replace the burst/cooldown fields on notification_rules with the new
-- silence/coalesce model. Existing rules reset to defaults — prior values
-- were products of the cooldown-doubling anti-pattern, not considered
-- operator choices. Operators re-tune after deploy.

ALTER TABLE notification_rules
    ADD COLUMN silence_ms     INTEGER NOT NULL DEFAULT 300000,  -- 5 minutes
    ADD COLUMN silence_max_ms INTEGER NOT NULL DEFAULT 900000,  -- 15 minutes
    ADD COLUMN coalesce_ms    INTEGER NOT NULL DEFAULT 0;

ALTER TABLE notification_rules
    DROP COLUMN burst_window,
    DROP COLUMN cooldown_seconds,
    DROP COLUMN max_cooldown_seconds;
