-- Mute rules: operator-created, time-boxed rules that mark matching netlog
-- or srvlog events as muted. Muting is evaluated at read and dispatch time,
-- so the event hypertables are untouched. A plain table: it holds tens of
-- rows. Rows are never deleted; unmuting sets expires_at to now.
--
-- A NULL match field is not matched on. created_by is a username snapshot,
-- not an FK, so it survives user deletion and covers the anonymous user.

CREATE TABLE mute_rules (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    feed         TEXT NOT NULL CHECK (feed IN ('netlog', 'srvlog')),
    hostname     TEXT,
    programname  TEXT,
    search       TEXT,
    reason       TEXT NOT NULL CHECK (length(btrim(reason)) > 0),
    created_by   TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ NOT NULL,
    CONSTRAINT mute_rules_match_required CHECK (num_nonnulls(hostname, programname, search) > 0)
);
