-- LDAP and OIDC sessions now last 12 hours instead of 30 days, so access the
-- directory withdraws ends within that time. Clamp the sessions issued before
-- this change to the same bound: a session older than 12 hours expires at
-- once and its user logs in again. Local sessions keep their 30 days.

UPDATE sessions s
SET expires_at = s.created_at + interval '12 hours'
FROM users u
WHERE u.id = s.user_id
  AND u.auth_source IN ('ldap', 'oidc')
  AND s.expires_at > s.created_at + interval '12 hours';
