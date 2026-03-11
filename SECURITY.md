# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| Latest  | Yes       |

## Reporting a Vulnerability

If you discover a security vulnerability in Taillight, please report it responsibly.

### Via GitHub Security Advisories (preferred)

1. Go to the [Security Advisories](https://github.com/lasseh/taillight/security/advisories) page
2. Click "Report a vulnerability"
3. Fill out the form with details about the vulnerability

### What to Expect

- **Acknowledgment** within 48 hours of your report
- **Status update** within 7 days with an assessment and timeline
- **Fix and disclosure** coordinated with you before public announcement

### Please Do Not

- Open public issues for security vulnerabilities
- Exploit the vulnerability beyond what is necessary to demonstrate it
- Share the vulnerability with others before it has been resolved

## Deployment Security Checklist

### Authentication and access control

- [ ] Set `auth_enabled: true` — disabling auth exposes all endpoints without login
- [ ] Set `auth_read_endpoints: true` — otherwise GET endpoints are public
- [ ] Set `cookie_secure: true` when serving over TLS (enforces `Secure` flag on session cookies)
- [ ] Do not set CORS `allowed_origins` to `["*"]` in production — this disables credential support and weakens origin checks
- [ ] Use strong, unique API keys for applog ingest (rotate periodically)
- [ ] Revoke unused API keys via the admin interface or CLI

### Network and TLS

- [ ] Deploy behind a reverse proxy (nginx, Caddy) with TLS termination — see `docs/nginx-reverse-proxy.conf.example`
- [ ] Bind the API server to `127.0.0.1` or a private interface; let the reverse proxy handle public traffic
- [ ] Restrict the `/metrics` endpoint to internal networks (Prometheus scraper only)
- [ ] Use firewall rules to limit access to PostgreSQL (port 5432) and rsyslog (port 514/1514)

### Secrets management

- [ ] Store database credentials in environment variables or a secrets manager, never in version-controlled files
- [ ] The `api/config.yml` file is gitignored — keep it that way; use `config.yml.example` as a template
- [ ] SMTP passwords and Slack webhook URLs belong in config or environment variables, not in notification rule payloads

### Database

- [ ] Use a dedicated PostgreSQL role for taillight with minimal privileges (SELECT, INSERT, UPDATE, DELETE on its own tables)
- [ ] Enable TLS for PostgreSQL connections in production (`sslmode=verify-full`)
- [ ] Configure retention policies on TimescaleDB hypertables to limit stored data volume

### Containers

- [ ] Both API and frontend Docker images run as non-root by default — do not override this
- [ ] Pin image tags to specific versions in production (avoid `latest`)
- [ ] Run `govulncheck` and Trivy container scanning in CI (already configured in `.github/workflows/`)

### Monitoring and incident response

- [ ] Monitor the `/metrics` endpoint for anomalies (error rates, auth failures, SSE client counts)
- [ ] Review API key `last_used_at` timestamps periodically to detect stale or compromised keys
- [ ] Enable structured JSON logging in production for centralized log aggregation
- [ ] Keep dependencies updated — `govulncheck` runs in CI and will flag known vulnerabilities
