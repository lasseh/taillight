# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/),
and this project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Note

Taillight was developed as an internal company tool before being open-sourced.
The git history was reset prior to the public release. All prior development
(features, bug fixes, and iterations) is captured in this initial release.

### Added

- Real-time syslog streaming via SSE with per-client server-side filtering
- Application log ingestion over HTTP (`POST /api/v1/applog/ingest`)
- Dashboard with aggregated volume charts and selectable time ranges
- Device-level log views with per-host statistics
- Cursor-based pagination for historical log browsing
- Notification engine with burst detection, cooldown, and rate limiting
- Slack, webhook, and email notification backends
- SSRF-safe webhook delivery with DNS rebinding protection
- Session-based authentication with bcrypt password hashing
- API key authentication with scoped access control (read, ingest, admin)
- Login rate limiting and session management
- Security headers middleware (CSP, HSTS, X-Frame-Options)
- Prometheus metrics endpoint with HTTP middleware instrumentation
- TimescaleDB hypertables for syslog and applog events
- Docker Compose setup with rsyslog, TimescaleDB, Go API, and Vue frontend
- CLI commands: serve, migrate, loadgen, applog-loadgen, useradd, apikey, import
- taillight-shipper: standalone log file tailer for ingest API
- logshipper: slog handler for shipping app logs to the ingest endpoint
- Multiple color themes (dark, light, nord, dracula, solarized, monokai, gruvbox)
- Vue 3 frontend with Tailwind CSS and real-time EventSource integration
