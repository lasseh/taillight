SELECT remove_retention_policy('rsyslog_stats', if_exists => true);
SELECT remove_compression_policy('rsyslog_stats', if_exists => true);
DROP TABLE IF EXISTS rsyslog_stats;
