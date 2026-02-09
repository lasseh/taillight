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

## Security Best Practices for Deployment

- Always deploy behind a reverse proxy (nginx) with TLS
- Use strong, unique API keys for applog ingest
- Restrict `/metrics` endpoint to internal networks
- Keep PostgreSQL credentials out of version control
- Regularly update dependencies (`govulncheck` is included in CI)
