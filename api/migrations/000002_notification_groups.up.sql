-- Add group_by and max_cooldown_seconds columns to notification_rules.
ALTER TABLE notification_rules
    ADD COLUMN group_by TEXT NOT NULL DEFAULT 'hostname',
    ADD COLUMN max_cooldown_seconds INTEGER NOT NULL DEFAULT 3600;

-- Change default cooldown from 300s (5m) to 60s (1m) for new rules.
ALTER TABLE notification_rules
    ALTER COLUMN cooldown_seconds SET DEFAULT 60;

-- Example Juniper notification rules (disabled, no channel associations).
INSERT INTO notification_rules
    (name, enabled, event_kind, search, severity_max, burst_window, cooldown_seconds, max_cooldown_seconds, group_by)
VALUES
    ('juniper-link-down',          false, 'syslog', 'SNMP_TRAP_LINK_DOWN',              3, 30, 60, 3600, 'hostname'),
    ('juniper-ospf-neighbor-down', false, 'syslog', 'RPD_OSPF_NBRDOWN',                 3, 30, 60, 3600, 'hostname'),
    ('juniper-bgp-peer-down',     false, 'syslog', 'RPD_BGP_NEIGHBOR_STATE_CHANGED',    4, 30, 60, 3600, 'hostname'),
    ('juniper-chassis-alarm',      false, 'syslog', 'CHASSISD_SNMP_TRAP',               3, 30, 60, 3600, 'hostname'),
    ('juniper-auth-failure',       false, 'syslog', 'SSHD_LOGIN_FAILED',                4, 30, 60, 3600, 'hostname'),
    ('juniper-kernel-panic',       false, 'syslog', 'KERNEL',                            2, 30, 60, 3600, 'hostname');
