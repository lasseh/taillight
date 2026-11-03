-- Reverse the external session clamp.
--
-- The up migration only shortened session expiries; the original values are
-- not kept, and a shorter session is safe to keep. Rolling back leaves the
-- clamped sessions as they are, and new sessions follow whatever lifetime
-- the running code sets.

SELECT 1;
