-- Remove example Juniper rules.
DELETE FROM notification_rules
WHERE name IN (
    'juniper-link-down',
    'juniper-ospf-neighbor-down',
    'juniper-bgp-peer-down',
    'juniper-chassis-alarm',
    'juniper-auth-failure',
    'juniper-kernel-panic'
);

-- Drop new columns.
ALTER TABLE notification_rules
    DROP COLUMN group_by,
    DROP COLUMN max_cooldown_seconds;

-- Restore original default cooldown.
ALTER TABLE notification_rules
    ALTER COLUMN cooldown_seconds SET DEFAULT 300;
