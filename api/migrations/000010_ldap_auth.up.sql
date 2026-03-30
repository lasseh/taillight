-- Add auth_source to distinguish local vs LDAP-provisioned users.
-- 'local' = bcrypt password in DB, 'ldap' = authenticated via LDAP bind.
ALTER TABLE users ADD COLUMN auth_source TEXT NOT NULL DEFAULT 'local';
