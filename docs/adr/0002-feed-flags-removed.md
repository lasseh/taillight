# Feed feature flags removed; all three feeds are always on

The `features.srvlog` / `features.netlog` / `features.applog` config flags were deleted (commit `cef04b1`). They had drifted into three different meanings — netlog: full gating (routes 404, broker+LISTEN off); srvlog: LISTEN-subscription only, so a disabled flag silently killed the live tail while REST kept serving; applog: frontend-echo only, with the ingest surface open regardless — while `config.go` and the README promised uniform "enable only the feeds you need" semantics. No shipped artifact ever disabled a feed (all defaults true everywhere), and a flag can never gate rsyslog-side ingest anyway: rsyslog writes to the events tables regardless of what the API serves.

Maintainer decision (architecture review 2026-07-02, D2): *"remove the feature flags for the 3 different logs, i always want them enabled. this was a poor decision earlier."* Deployed configs that still carry a `features:` block are unaffected — viper ignores unknown keys.

`GET /api/v1/config/features` survives because frontend routing is load-bearing on it; the three feed keys now report `true` as constants alongside the real `analysis` flag, which remains the one genuinely optional UI surface.

Reopen only if a real deployment needs a feed off — and then implement full netlog-style gating (routes + broker + LISTEN + frontend) in one piece, never a partial wiring. Half-gated flags are worse than none: they lie.

See `.scratch/architecture-review/REPORT.md` §5.3 / D2.
