-- Drop the standalone applog_events id index. DROP INDEX is a catalog-only
-- operation with a brief lock, so it does not need the per-chunk treatment of
-- the up migration.
DROP INDEX IF EXISTS idx_applog_id;
