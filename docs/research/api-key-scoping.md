# API Key Scoping

## Status: Implemented

API keys now carry a `scopes` field that restricts what operations each key can perform. Static config-based API keys (`api_keys` in config.yml) have been removed in favor of DB-backed keys with scopes.

## Scope Levels

| Scope | Access |
|-------|--------|
| `ingest` | `POST /api/v1/applog/ingest` only |
| `read` | All GET endpoints (syslog, applog, meta, stats, volume, analysis, notifications, juniper, rsyslog, metrics) |
| `admin` | Full access — everything read can do, plus POST/PUT/DELETE on notifications, analysis trigger, user management |

Scopes are additive. A key with `["read", "ingest"]` can read data and ingest applog events but cannot manage notification rules.

Session-based auth (cookie login) always gets full access — no scope restriction applies.

## Schema

```sql
ALTER TABLE api_keys ADD COLUMN scopes TEXT[] NOT NULL DEFAULT '{}';
```

Migration: `000004_api_key_scopes.up.sql`

## Implementation

### Middleware (`auth/middleware.go`)

- `SessionOrAPIKey` stores both user and scopes in context (scopes are nil for session auth).
- `RequireScope(scope)` middleware checks context scopes:
  - nil scopes (session auth) → allow
  - scopes contain `"admin"` → allow
  - scopes contain required scope → allow
  - otherwise → 403 Forbidden

### Route structure (`serve.go`)

Routes are grouped by scope:
- **Read group**: all GET endpoints wrapped with `RequireScope("read")`
- **Ingest group**: `POST /applog/ingest` wrapped with `RequireScope("ingest")`
- **Admin group**: write operations (POST/PUT/DELETE notifications, analysis trigger) wrapped with `RequireScope("admin")`
- **Auth self-management**: `/auth/me`, `/auth/keys` — no scope required (any authenticated user)

### API

Creating a key requires scopes:

```json
POST /api/v1/auth/keys
{
  "name": "CI ingest key",
  "scopes": ["ingest"],
  "expires_at": "2026-01-01T00:00:00Z"
}
```

Valid scope values: `ingest`, `read`, `admin`. At least one scope is required.

### CLI

```
taillight apikey --username admin --name "my-key" --scopes ingest,read
```

Default scope when `--scopes` is omitted: `admin`.
