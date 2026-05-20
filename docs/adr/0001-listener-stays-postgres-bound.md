# Listener stays Postgres-bound; only the dispatch step is portable

We deliberately did **not** introduce a ports-and-adapters seam around `postgres.Listener` (LISTEN/NOTIFY receive, reconnect/backoff, gap-fill query). Mocking `pgx.Conn.WaitForNotification` carries a heavier cost than it returns, and the gap-fill SQL is genuinely coupled to Postgres — the Listener is a deep module whose depth comes from owning that coupling. Only the shallow, previously-untested **dispatch step** (NOTIFY channel switch → fetch by id → broadcast → engine handoff) was extracted behind a small `EventFetcher` port in `internal/ingestbridge`, which is what made it unit-testable without a database.

If a future architecture review proposes a full Listener port, reopen this only if (a) we acquire a second non-Postgres notification source, or (b) the gap-fill logic can be expressed without SQL — otherwise the cost/benefit ratio has not changed.

See commit `f399606` for the dispatch extraction.
