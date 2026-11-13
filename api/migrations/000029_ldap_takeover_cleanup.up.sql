-- An LDAP login that takes over a same-name row now ends the previous
-- holder's sessions and API keys and clears the row's OIDC link. Rows taken
-- over before that change still carry the link. Clean them in the same order
-- the login does: sessions, then keys, then the link, because the link is the
-- only marker that identifies these rows.
--
-- A local row taken over before the change looks the same as a person moving
-- from a local account to LDAP, so it is not touched here; review rows with
-- auth_source = 'ldap' AND password_hash <> '' by hand.

DELETE FROM sessions
WHERE user_id IN (SELECT id FROM users WHERE auth_source <> 'oidc' AND oidc_issuer IS NOT NULL);

UPDATE api_keys SET revoked_at = now()
WHERE revoked_at IS NULL
  AND user_id IN (SELECT id FROM users WHERE auth_source <> 'oidc' AND oidc_issuer IS NOT NULL);

UPDATE users SET oidc_issuer = NULL, oidc_subject = NULL, updated_at = now()
WHERE auth_source <> 'oidc' AND oidc_issuer IS NOT NULL;
