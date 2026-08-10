// Runtime configuration, read by lib/config.ts. docker-entrypoint.sh overwrites
// this file at container start when API_URL is set; the default below leaves the
// API URL empty, which means same-origin (proxy mode).
//
// It ships as a real file — rather than being injected into index.html — so the
// CSP needs no 'unsafe-inline', and so requesting it never 404s.
window.__CONFIG__ = {}
