-- Force every retention policy back to its intended drop_after.
--
-- Migrations 2, 3, 4, 6 and 8 all create retention policies with
-- `add_retention_policy(..., if_not_exists => true)`. That flag makes the call
-- a no-op once *any* policy exists on the table — including one with different
-- arguments. Postgres says so outright:
--
--     WARNING:  retention policy already exists for hypertable "netlog_events"
--     DETAIL:   A policy already exists with different arguments.
--     HINT:     Remove the existing policy before adding a new one.
--
-- The call returns -1, the migration reports success, and the drifted policy
-- survives. Nothing in the schema history can ever correct it.
--
-- A production database hit exactly that. netlog_events held 120 daily chunks
-- with no gaps — 119 days of data under a nominal 90-day policy — while its
-- retention job reported Success every day with zero failures, because the
-- job had been dropped and re-added out-of-band with a different drop_after.
-- The recreated job's id was far out of sequence with the other policy jobs
-- created alongside it, which is the fingerprint of this drift.
--
-- Remove-then-add is already what the columnstore policies in those same
-- migrations do (`CALL remove_columnstore_policy(...)` immediately followed by
-- `CALL add_columnstore_policy(...)`), and it is the form TimescaleDB's own
-- hint asks for. Using it here forces every existing database onto the
-- intended values when it upgrades, which `if_not_exists` could not do.
--
-- This runs once, like any migration — it is not a standing repair. If a
-- policy is changed out-of-band again, only another migration will correct
-- it. What this does buy is that the correction is now expressible at all.
--
-- Re-adding a policy resets its job id and its job_stats counters. That is
-- cosmetic: the schedule and the drop_after are what govern behaviour, and
-- both are restated here.

-------------------------------------------------------------------------------
-- Event hypertables — 90 days
-------------------------------------------------------------------------------

SELECT remove_retention_policy('srvlog_events', if_exists => true);
SELECT add_retention_policy('srvlog_events', INTERVAL '90 days');

SELECT remove_retention_policy('netlog_events', if_exists => true);
SELECT add_retention_policy('netlog_events', INTERVAL '90 days');

SELECT remove_retention_policy('applog_events', if_exists => true);
SELECT add_retention_policy('applog_events', INTERVAL '90 days');

-------------------------------------------------------------------------------
-- Telemetry and audit hypertables — 30 days
-------------------------------------------------------------------------------

SELECT remove_retention_policy('notification_log', if_exists => true);
SELECT add_retention_policy('notification_log', INTERVAL '30 days');

SELECT remove_retention_policy('rsyslog_stats', if_exists => true);
SELECT add_retention_policy('rsyslog_stats', INTERVAL '30 days');

SELECT remove_retention_policy('taillight_metrics', if_exists => true);
SELECT add_retention_policy('taillight_metrics', INTERVAL '30 days');
