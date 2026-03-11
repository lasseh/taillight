-- Reverse notifications migration (FK order).

SELECT remove_retention_policy('notification_log', if_exists => true);
DROP TABLE IF EXISTS notification_log;
DROP TABLE IF EXISTS notification_rule_channels;
DROP TABLE IF EXISTS notification_rules;
DROP TABLE IF EXISTS notification_channels;
