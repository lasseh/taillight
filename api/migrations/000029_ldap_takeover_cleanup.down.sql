-- The up migration deletes sessions, revokes API keys and clears OIDC links.
-- None of that can be restored, so down is a no-op.
SELECT 1;
