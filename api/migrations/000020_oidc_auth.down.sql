DROP INDEX IF EXISTS users_oidc_identity_idx;
ALTER TABLE users DROP COLUMN IF EXISTS oidc_subject;
ALTER TABLE users DROP COLUMN IF EXISTS oidc_issuer;
