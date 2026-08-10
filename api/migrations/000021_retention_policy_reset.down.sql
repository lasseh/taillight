-- Reverse the retention policy reset.
--
-- The up migration only restates the drop_after values that migrations 2, 3,
-- 4, 6 and 8 already intended, so there is no prior state to restore — the
-- state it replaced was drift. Dropping the policies here would be worse than
-- the disease: it would leave the tables growing without bound.
--
-- Rolling back to before migration 21 therefore leaves the policies in place,
-- exactly as they were defined by the migrations that own each table. Those
-- migrations' own down files remove their policies when they are rolled back.

SELECT 1;
