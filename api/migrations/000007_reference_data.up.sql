-- Reference data: Juniper syslog reference + seed notification rules.

-------------------------------------------------------------------------------
-- 1. Juniper syslog reference table
-------------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS juniper_netlog_ref (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name        TEXT NOT NULL,
    message     TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    type        TEXT NOT NULL DEFAULT '',
    severity    TEXT NOT NULL DEFAULT '',
    cause       TEXT NOT NULL DEFAULT '',
    action      TEXT NOT NULL DEFAULT '',
    os          TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_juniper_ref_name_os ON juniper_netlog_ref (name, os);
CREATE INDEX IF NOT EXISTS idx_juniper_ref_name ON juniper_netlog_ref (name);

-------------------------------------------------------------------------------
-- 2. Example Juniper notification rules (disabled, no channel associations)
-------------------------------------------------------------------------------

INSERT INTO notification_rules
    (name, enabled, event_kind, search, severity_max, burst_window, cooldown_seconds, max_cooldown_seconds, group_by)
VALUES
    ('juniper-link-down',          false, 'netlog', 'SNMP_TRAP_LINK_DOWN',              3, 30, 60, 3600, 'hostname'),
    ('juniper-ospf-neighbor-down', false, 'netlog', 'RPD_OSPF_NBRDOWN',                 3, 30, 60, 3600, 'hostname'),
    ('juniper-bgp-peer-down',     false, 'netlog', 'RPD_BGP_NEIGHBOR_STATE_CHANGED',    4, 30, 60, 3600, 'hostname'),
    ('juniper-chassis-alarm',      false, 'netlog', 'CHASSISD_SNMP_TRAP',               3, 30, 60, 3600, 'hostname'),
    ('juniper-auth-failure',       false, 'netlog', 'SSHD_LOGIN_FAILED',                4, 30, 60, 3600, 'hostname'),
    ('juniper-kernel-panic',       false, 'netlog', 'KERNEL',                            2, 30, 60, 3600, 'hostname');
