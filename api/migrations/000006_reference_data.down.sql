-- Reverse reference data migration.

-- Remove seed notification rules.
DELETE FROM notification_rules
WHERE name IN (
    'juniper-link-down',
    'juniper-ospf-neighbor-down',
    'juniper-bgp-peer-down',
    'juniper-chassis-alarm',
    'juniper-auth-failure',
    'juniper-kernel-panic'
);

-- Drop reference table.
DROP TABLE IF EXISTS juniper_syslog_ref;
