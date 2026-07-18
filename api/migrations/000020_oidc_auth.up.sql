-- OIDC identity columns. An OIDC user is keyed on (issuer, subject) — the
-- only stable OIDC identifier; username/email are mutable profile data.
-- NULL for local and LDAP users.
ALTER TABLE users ADD COLUMN oidc_issuer TEXT;
ALTER TABLE users ADD COLUMN oidc_subject TEXT;

CREATE UNIQUE INDEX users_oidc_identity_idx ON users (oidc_issuer, oidc_subject)
    WHERE oidc_issuer IS NOT NULL;
