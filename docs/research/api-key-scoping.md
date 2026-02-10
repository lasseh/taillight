# API Key Scoping

## Overview

Taillight currently treats all API keys as equivalent — any valid key grants full access to every endpoint. This is a security concern in production environments where different consumers need different access levels. An ingest agent should only be able to POST applog events, not read syslog data or manage notification rules. A dashboard display should only read, not write.

API key scoping adds a `scopes` field to API keys that restricts what operations each key can perform, following the principle of least privilege.

### What this gives us

- **Ingest-only keys** for external services (CI/CD, application agents, log shippers) — they can POST applog events but cannot read any data. If a key is compromised, the attacker can only send garbage logs, not exfiltrate data.
- **Read-only keys** for dashboards and monitoring integrations — they can query events and stats but cannot modify notification rules, manage users, or ingest data.
- **Admin keys** for management tooling — full access, equivalent to the current behavior.
- **Audit trail** — scoped keys make it clear *what* each key is intended for, aiding in key lifecycle management and incident response.

## Current State

### API key model

`model/auth.go:33-44` defines `APIKeyRow`:

```go
type APIKeyRow struct {
    ID         pgtype.UUID        `json:"id"`
    UserID     pgtype.UUID        `json:"user_id"`
    Name       string             `json:"name"`
    KeyHash    string             `json:"-"`
    KeyPrefix  string             `json:"key_prefix"`
    ExpiresAt  pgtype.Timestamptz `json:"expires_at,omitempty"`
    RevokedAt  pgtype.Timestamptz `json:"revoked_at,omitempty"`
    LastUsedAt pgtype.Timestamptz `json:"last_used_at,omitempty"`
    CreatedAt  time.Time          `json:"created_at"`
}
```

No scopes field exists.

### API key storage

`auth_store.go:226-284` handles API key CRUD. `CreateAPIKey` (`auth_store.go:229-242`) inserts with `user_id, name, key_hash, key_prefix, expires_at`. `GetAPIKeyByHash` (`auth_store.go:252-284`) joins with users and validates revocation/expiry — but performs no scope check.

### API key schema

`migrations/000001_init_schema.up.sql:272-284`:

```sql
CREATE TABLE IF NOT EXISTS api_keys (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 255),
    key_hash     TEXT NOT NULL,
    key_prefix   TEXT NOT NULL DEFAULT '',
    expires_at   TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### Authentication middleware

`auth/middleware.go:49-84` (`SessionOrAPIKey`) authenticates requests via session cookie or Bearer token. On success, it stores a `*model.User` in the context. There is no authorization step — any authenticated request is fully authorized.

The middleware flow:

1. Try session cookie → store user in context
2. Try DB-backed API key (prefix `tl_`) → store user in context
3. Fall back to config-based API keys → pass through without user context

Config-based keys (step 3) have no user association and therefore cannot carry scopes. They would continue to operate as admin-level keys.

## Proposed Design

### Scope levels

| Scope | Access |
|-------|--------|
| `ingest` | `POST /api/v1/applog/ingest` only |
| `read` | All `GET` endpoints (events, meta, stats, volume, health) |
| `notify` | Read + notification rule/channel management |
| `admin` | Full access (equivalent to current behavior) |

Scopes are additive. A key with `["read", "ingest"]` can read data and ingest applog events but cannot manage notification rules.

### Schema change

```sql
-- New migration
ALTER TABLE api_keys ADD COLUMN scopes TEXT[] NOT NULL DEFAULT '{admin}';
```

Using `TEXT[]` (Postgres array) rather than JSONB for simpler queries:

```sql
-- Check if key has required scope
WHERE scopes @> ARRAY['read']
```

Default `{admin}` preserves backward compatibility — existing keys get full access.

### Model change

```go
type APIKeyRow struct {
    // ... existing fields ...
    Scopes []string `json:"scopes"`
}
```

### Store changes

`auth_store.go` — update `CreateAPIKey` to accept scopes, update all SELECT queries to include the `scopes` column, update scan calls.

The `GetAPIKeyByHash` method (`auth_store.go:252-284`) already joins with users. Add `scopes` to the SELECT and scan.

### Middleware changes

After key validation in `middleware.go:46-84`, add a scope check. Two approaches:

**Option A: Middleware per scope** — Wrap route groups with scope-checking middleware:

```go
r.Route("/api/v1", func(r chi.Router) {
    r.Use(auth.SessionOrAPIKey(sessions, apiKeys, configKeys))

    // Read-scoped routes
    r.Group(func(r chi.Router) {
        r.Use(auth.RequireScope("read"))
        r.Get("/syslog", handler.ListSyslogs)
        r.Get("/syslog/stream", handler.StreamSyslogs)
        // ...
    })

    // Ingest-scoped routes
    r.Group(func(r chi.Router) {
        r.Use(auth.RequireScope("ingest"))
        r.Post("/applog/ingest", handler.IngestAppLog)
    })

    // Admin-scoped routes
    r.Group(func(r chi.Router) {
        r.Use(auth.RequireScope("admin"))
        r.Post("/users", handler.CreateUser)
        // ...
    })
})
```

**Option B: Store scopes in context, check per handler** — More flexible but disperses authorization logic.

Recommendation: Option A. It keeps authorization centralized in the router and makes scope requirements visible in the route definition.

### RequireScope middleware

```go
func RequireScope(scope string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            user := UserFromContext(r.Context())
            if user == nil {
                // Session-based auth (no scopes) — allow
                // Config-based keys — allow
                next.ServeHTTP(w, r)
                return
            }
            if user.APIKeyScopes == nil {
                // Session login — full access
                next.ServeHTTP(w, r)
                return
            }
            if !hasScope(user.APIKeyScopes, scope) {
                writeJSONError(w, http.StatusForbidden, "forbidden",
                    fmt.Sprintf("api key missing required scope: %s", scope))
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

The `admin` scope grants access to everything (checked in `hasScope`).

### Context changes

Scopes need to reach the middleware. Options:

1. Add `APIKeyScopes []string` to `model.User` (simplest, slightly overloads the User type).
2. Store scopes separately in context via a new context key.

Option 1 is simpler since `User` already carries auth info (`IsAdmin`). The scopes field would be nil for session-based auth (meaning full access) and populated for API key auth.

### API surface

Update the existing API key endpoints:

```
POST /api/v1/auth/api-keys
```

Request body gains `scopes`:

```json
{
  "name": "CI ingest key",
  "scopes": ["ingest"],
  "expires_at": "2026-01-01T00:00:00Z"
}
```

If `scopes` is omitted, default to `["admin"]` for backward compatibility.

`GET /api/v1/auth/api-keys` response includes scopes:

```json
{
  "data": [
    {
      "id": "...",
      "name": "CI ingest key",
      "key_prefix": "tl_abc...",
      "scopes": ["ingest"],
      "created_at": "..."
    }
  ]
}
```

## Implementation Notes

### Files to modify

| File | Change |
|------|--------|
| `migrations/000004_api_key_scopes.up.sql` | Add `scopes` column with default |
| `internal/model/auth.go:33-44` | Add `Scopes []string` to `APIKeyRow`, optionally `APIKeyScopes` to `User` |
| `internal/postgres/auth_store.go:229-284` | Update CREATE/SELECT/SCAN to include scopes |
| `internal/auth/middleware.go:49-84` | Store scopes in context, add `RequireScope` middleware |
| `cmd/taillight/serve.go` | Wrap route groups with scope middleware |
| Frontend API key management page | Add scope selection UI |

### Migration safety

The `DEFAULT '{admin}'` ensures all existing keys get full access. No data migration needed. The migration is backward-compatible — the application works identically until `RequireScope` middleware is applied.

### Config-based keys

Config-based API keys (from `config.yaml`, matched by `constantTimeMatch` in `middleware.go:100-108`) have no database row and therefore no scopes. They should continue to grant full access — they're typically used for initial setup or simple deployments.

## Open Questions

1. **Scope granularity?** `read`/`ingest`/`notify`/`admin` is coarse. Is per-endpoint scoping needed (e.g., `read:syslog` vs. `read:applog`)?
2. **Should session-based auth also have scopes?** Currently sessions grant full access. Adding role-based access (viewer/editor/admin) to users is a separate feature but could share the scope infrastructure.
3. **Scope on key creation vs. key update?** Should scopes be immutable after creation (more secure — revoke and recreate), or updatable?
4. **How to handle config-based keys?** Leave as admin-level, or deprecate in favor of DB-backed keys?
