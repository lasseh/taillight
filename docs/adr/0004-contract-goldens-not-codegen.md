# Contract integrity via golden fixtures in CI, not codegen or runtime validation

The API contract has four consumers (Vue SPA, taillight-tui, `pkg/logshipper`, `sdk/python`) and lived in six hand-maintained copies with zero mechanical checks. The 2026-07 review confirmed real drift (phantom TS fields, absent-vs-null mismatches from `omitempty`, stale TUI decode shapes, a 6-weeks-stale OpenAPI spec).

Decision (D5): **golden-fixture contract tests**, scoped to the highest-churn surfaces — the three event shapes (including nil-field and `attrs_truncated` variants), the `{data, cursor, has_more}` list/detail envelopes, and the applog ingest request validated against the real server rules. Go marshals canonical fixtures into checked-in JSON (`api/internal/handler/testdata/`); vitest asserts the same files against the TS types. Landed in commit `e0f23e5`.

Rejected with reasons:
- **Codegen (tygo / OpenAPI-first)** — the SPA's hand-maintained types were the *healthiest* copy audited; codegen adds a permanent toolchain to solve the smallest slice, and OpenAPI-first would crown the most-rotted artifact as the source of truth.
- **Runtime validation (zod/valibot)** — converts silent type-lies into live-feed runtime failures; the worst failure mode for a NOC tool. CI goldens catch the same drift before deploy.

`docs/openapi.yml` is kept truthful separately: backfilled to the real route set, with a route-inventory walker test so a new route without a spec entry fails CI.

Reopen if consumer count grows to where fixture maintenance dominates (then revisit codegen with the goldens as its seed corpus).

See `.scratch/architecture-review/REPORT.md` D5.
