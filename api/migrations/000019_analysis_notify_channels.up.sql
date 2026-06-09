-- Per-schedule analysis-report email targets.
--
-- A schedule references existing email notification channels by id; the report
-- snapshots those ids at enqueue time so editing or deleting a schedule
-- afterward cannot change which channels a pending report dispatches to
-- (mirrors how feed/mode/hosts are already snapshotted onto the report row).
-- The channel's contents (recipients, subject, attach_pdf) are resolved live
-- from notification_channels at send time.
--
-- Plain BIGINT[] rather than a join table with FKs: the report needs an array
-- snapshot regardless, a deleted channel is simply skipped at send time, and
-- the validated set is small. Canonical "no targets" is the empty array.
ALTER TABLE analysis_schedules
    ADD COLUMN notify_channel_ids BIGINT[] NOT NULL DEFAULT '{}';

ALTER TABLE analysis_reports
    ADD COLUMN notify_channel_ids BIGINT[] NOT NULL DEFAULT '{}';
