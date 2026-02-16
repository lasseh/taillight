# Security Audit Report — Taillight

**Date:** 2026-02-09
**Scope:** Full codebase (`api/`, `frontend/`, `rsyslog/`, `docker-compose.yml`)
**Commit:** `f69a05d` (post-remediation)

---

## Executive Summary

The Taillight codebase demonstrates strong security fundamentals: all SQL queries use parameterized statements, passwords are hashed with bcrypt, API key comparison uses constant-time functions, error responses never leak internal details, and structured logging avoids dumping sensitive data.

The audit found **no critical vulnerabilities**. Five HIGH/MEDIUM issues were identified and fixed in commit `f69a05d`. Seven additional MEDIUM/LOW/INFO findings are documented below as accepted risks or future improvements.

---

## Findings

### FIXED — Remediated in `f69a05d`

#### F-1: SSE read endpoints unauthenticated by default (HIGH)

| | |
|---|---|
| **Severity** | HIGH |
| **Category** | Authentication |
| **File** | `api/internal/config/config.go:69` |
| **Status** | Fixed |

**Description:** The `auth_read_endpoints` config defaulted to `false`, meaning SSE streams and all read-only API endpoints were publicly accessible without authentication, even when `auth_enabled` was `true`. An unauthenticated user could view live syslog and applog data.

**Fix:** Changed default from `false` to `true`. Read endpoints now require authentication by default. Operators who need public read access must explicitly set `auth_read_endpoints: false` in their config.

**Affected code:**
- `api/internal/config/config.go:69` — default value
- `api/cmd/taillight/serve.go:366-368` — conditional middleware application

---

#### F-2: No SSE connection limits (HIGH)

| | |
|---|---|
| **Severity** | HIGH |
| **Category** | Denial of Service |
| **Files** | `api/internal/broker/syslog_broker.go`, `api/internal/broker/applog_broker.go` |
| **Status** | Fixed |

**Description:** Both SSE brokers accepted unlimited subscriber connections. Each subscription allocates a buffered channel (64 slots of serialized JSON). An attacker could open thousands of SSE connections to exhaust server memory.

**Fix:** Added a `maxSubscribers = 1000` cap per broker. `Subscribe()` now returns `ErrTooManySubscribers` when the limit is reached. SSE handlers respond with `503 Service Unavailable` and a `too_many_connections` error code.

**Affected code:**
- `api/internal/broker/syslog_broker.go:14-25` — constant and sentinel error
- `api/internal/broker/syslog_broker.go:61-76` — limit check in `Subscribe()`
- `api/internal/broker/applog_broker.go:45-59` — same pattern
- `api/internal/handler/syslog_sse.go:52-56` — error handling in handler
- `api/internal/handler/applog_sse.go:51-55` — error handling in handler

---

#### F-3: Missing HSTS header (MEDIUM)

| | |
|---|---|
| **Severity** | MEDIUM |
| **Category** | Transport Security |
| **File** | `api/internal/handler/security_headers.go:13` |
| **Status** | Fixed |

**Description:** The `SecurityHeaders` middleware set `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, `Permissions-Policy`, and `Content-Security-Policy` but did not include `Strict-Transport-Security` (HSTS). Without HSTS, browsers allow initial connections over plain HTTP, enabling SSL-stripping attacks.

**Fix:** Added `Strict-Transport-Security: max-age=63072000; includeSubDomains` (2 years). This instructs browsers to only connect via HTTPS after the first visit.

---

#### F-4: Bcrypt cost factor below modern recommendation (MEDIUM)

| | |
|---|---|
| **Severity** | MEDIUM |
| **Category** | Cryptography |
| **File** | `api/internal/auth/auth.go:15` |
| **Status** | Fixed |

**Description:** Bcrypt cost was set to 10 (the Go `bcrypt` package default). OWASP and NIST recommend a minimum cost of 12 for current hardware. Cost 10 takes ~100ms on modern CPUs; cost 12 takes ~400ms, making offline brute-force significantly more expensive.

**Fix:** Changed `bcryptCost` from 10 to 12. This only affects newly created or changed passwords — existing hashes remain valid at their original cost and will be upgraded on next password change. The `dummyHash` (used for timing-safe username enumeration protection) is also regenerated at cost 12.

---

#### F-5: Nginx missing request body size limit (MEDIUM)

| | |
|---|---|
| **Severity** | MEDIUM |
| **Category** | Denial of Service |
| **File** | `frontend/nginx.conf` |
| **Status** | Fixed |

**Description:** The nginx reverse proxy had no `client_max_body_size` directive. Nginx defaults to 1MB, but this should be explicitly set to prevent ambiguity and ensure the limit applies even if the default changes.

**Fix:** Added `client_max_body_size 10m;` in the server block. This is generous for JSON API payloads while preventing multi-gigabyte uploads from reaching the Go backend.

---

### DOCUMENTED — Accepted Risks / Future Improvements

#### F-6: No rate limiting on login endpoint (MEDIUM)

| | |
|---|---|
| **Severity** | MEDIUM |
| **Category** | Brute Force |
| **File** | `api/internal/handler/auth.go:120-201` |
| **Status** | Accepted risk |

**Description:** The `/api/v1/auth/login` endpoint has no rate limiting. An attacker can attempt unlimited login attempts. The bcrypt cost factor (now 12) provides natural throttling (~400ms per attempt), and the dummy password check prevents username enumeration, but dedicated brute-force attacks are still feasible.

**Mitigations in place:**
- Bcrypt cost 12 limits throughput to ~2.5 attempts/sec per CPU core
- `DummyCheckPassword()` prevents timing-based username enumeration
- Session pruning caps sessions per user to 10

**Recommendation:** Add per-IP rate limiting using `golang.org/x/time/rate` or a middleware like `httprate`. A reasonable limit would be 10 attempts per minute per IP. This requires a new dependency and was deferred.

---

#### F-7: CORS falls back to localhost origins (MEDIUM)

| | |
|---|---|
| **Severity** | MEDIUM |
| **Category** | Configuration |
| **File** | `api/cmd/taillight/serve.go:290-292` |
| **Status** | Accepted risk (by design) |

**Description:** When `cors_allowed_origins` is empty, the server silently defaults to `http://localhost:5173` and `http://localhost:3000` (Vite and CRA dev servers). A warning is logged. In production, if CORS is misconfigured, the fallback exposes the API to any localhost origin.

**Mitigations in place:**
- Warning logged: `"CORS defaulting to localhost dev origins — set cors_allowed_origins for production"`
- Wildcard origin detection prevents `AllowCredentials: true` with `*`

**Recommendation:** This is an intentional developer-experience trade-off. No code change needed, but operators should always set `cors_allowed_origins` explicitly in production configs.

---

#### F-8: rsyslog container runs as root (MEDIUM)

| | |
|---|---|
| **Severity** | MEDIUM |
| **Category** | Container Security |
| **File** | `rsyslog/Dockerfile` |
| **Status** | Accepted risk |

**Description:** The rsyslog Docker container runs as root. This is a broader attack surface if the container is compromised.

**Justification:** rsyslog must bind to privileged port 514 (UDP/TCP) for syslog reception. Running as a non-root user would require `CAP_NET_BIND_SERVICE` capabilities or port remapping, adding operational complexity. The container has minimal packages installed (debian-slim + rsyslog only).

**Recommendation:** If the deployment environment supports it, consider using `--cap-add=NET_BIND_SERVICE` with a non-root user in the future.

---

#### F-9: CSP allows `unsafe-inline` for styles (LOW)

| | |
|---|---|
| **Severity** | LOW |
| **Category** | Content Security |
| **File** | `api/internal/handler/security_headers.go:12` |
| **Status** | Accepted risk |

**Description:** The Content-Security-Policy includes `style-src 'self' 'unsafe-inline'`, which allows inline `<style>` blocks and `style=""` attributes. This weakens the CSP against CSS injection attacks.

**Justification:** Many UI frameworks (including the frontend build pipeline) inject inline styles at runtime. Removing `unsafe-inline` would require nonce-based CSP, which adds complexity to the server-rendered responses.

**Recommendation:** If the frontend is migrated to a framework that supports CSP nonces or hashes, remove `unsafe-inline` at that time.

---

#### F-10: No CSRF tokens (LOW)

| | |
|---|---|
| **Severity** | LOW |
| **Category** | Cross-Site Request Forgery |
| **Status** | Accepted risk |

**Description:** The application does not use CSRF tokens for state-changing requests. Cross-site forms could potentially trigger POST/PATCH/DELETE actions if a user is authenticated.

**Mitigations in place:**
- Session cookie uses `SameSite=Lax`, which blocks cross-origin POST requests from third-party sites in all modern browsers
- `HttpOnly` flag prevents JavaScript access to the session cookie
- `Secure` flag is set dynamically based on the transport protocol
- CORS policy restricts cross-origin requests to configured origins

**Recommendation:** `SameSite=Lax` provides adequate protection for this application's threat model. CSRF tokens would be warranted if `SameSite=None` is ever needed (e.g., for cross-origin iframe embedding).

---

#### F-11: Password minimum length is 8 characters (LOW)

| | |
|---|---|
| **Severity** | LOW |
| **Category** | Authentication |
| **File** | `api/internal/handler/auth.go:497` |
| **Status** | Accepted risk |

**Description:** The password change endpoint enforces a minimum of 8 characters. NIST SP 800-63B recommends a minimum of 8 (with 12+ preferred). For an internal log management tool, 8 is a reasonable floor.

**Recommendation:** Consider bumping to 12 characters if the user base expands beyond internal teams. No password complexity rules (uppercase, numbers, symbols) are enforced, which aligns with current NIST guidance that discourages complexity requirements in favor of length.

---

#### F-12: `fmt.Sprintf` SQL in rsyslog stats store (INFO)

| | |
|---|---|
| **Severity** | INFO |
| **Category** | SQL Construction |
| **File** | `api/internal/postgres/rsyslog_stats_store.go:48-62, 184-231` |
| **Status** | Accepted risk |

**Description:** The `GetRsyslogStatsSummary` and `GetRsyslogStatsTimeSeries` functions use `fmt.Sprintf` to build SQL queries with the `innerStatsExpr` constant and field expressions. While this pattern looks like SQL injection at first glance, it is safe because:

1. `innerStatsExpr` is a compile-time constant (`(stats ->> 'msg')::jsonb`)
2. Field names are validated against `allowedStatsFields` whitelist (line 174)
3. All user-supplied values (`since`, `interval`) are passed as parameterized `$1`/`$2` placeholders
4. The `interval` value is validated via `interval.IsValid()` before use

**Recommendation:** No immediate action needed. If the stats store grows more complex, consider refactoring to a query builder. The current approach is safe but requires discipline when adding new fields.

---

## Positive Security Findings

The following security controls were found to be correctly implemented:

| Control | Location | Notes |
|---------|----------|-------|
| **Parameterized SQL** | All store files | No raw string interpolation with user input |
| **Bcrypt password hashing** | `auth/auth.go` | Cost 12, with dummy check for timing safety |
| **Constant-time API key comparison** | `auth/middleware.go` | `subtle.ConstantTimeCompare` for config keys |
| **Session token hashing** | `auth/auth.go` | SHA-256 hash stored in DB, raw token in cookie |
| **Request body limits** | `handler/auth.go:27` | `MaxBytesReader` on all auth endpoints (4KB) |
| **Session pruning** | `handler/auth.go:179` | Max 10 sessions per user, expired sessions cleaned |
| **Safe error responses** | `handler/response.go` | Generic error codes, no stack traces or internal details |
| **Structured logging** | All handlers | `LoggerFromContext()` pattern, no sensitive data logged |
| **Security headers** | `handler/security_headers.go` | Full set: CSP, HSTS, X-Frame, X-Content-Type, Referrer, Permissions |
| **Cookie security** | `handler/auth.go:188-196` | `HttpOnly`, `Secure` (dynamic), `SameSite=Lax` |
| **Graceful shutdown** | `cmd/taillight/serve.go` | Signal handling, connection draining, broker shutdown |
| **ReadHeaderTimeout** | `cmd/taillight/serve.go:94` | 10s — prevents slowloris attacks |
| **CORS wildcard guard** | `cmd/taillight/serve.go:294-306` | Credentials disabled when `*` origin is present |
| **Input validation** | `model/filter.go` | Strict parsing for severity, facility, time ranges, IP addresses |
| **LIKE escaping** | `postgres/store.go` | `escapeLike()` function prevents LIKE metacharacter injection |

---

## Architecture Notes

- **Authentication flow:** Cookie-based sessions with bcrypt + SHA-256 token hashing. API keys use `tl_` prefix + 43 base62 chars, stored as SHA-256 hashes. Both paths converge in the `SessionOrAPIKey` middleware.
- **SSE design:** Subscribe-before-backfill pattern avoids race conditions. Slow clients get events dropped (non-blocking send to buffered channel) rather than blocking the broker. Prometheus metrics track active clients, broadcast totals, and dropped events.
- **Database:** PostgreSQL with pgx connection pool. TimescaleDB hypertables for time-series data. LISTEN/NOTIFY for real-time event bridging.
- **No sensitive data in logs:** Error messages use generic codes. Database URLs, API keys, and passwords are never logged.
