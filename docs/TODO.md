# Future Optimizations

## ~~Batch INSERT for applog ingest~~ ✅ DONE

~~The current applog ingest handler inserts rows one-at-a-time inside a transaction.~~

**Implemented:** The applog ingest handler now uses `pgx.Batch` API for efficient batch inserts. All events in a single ingest request are queued and executed in one database round-trip.

See `internal/postgres/applog_store.go:InsertLogBatch()` for the implementation.

## Rate limiting on ingest endpoint

The `/api/v1/applog/ingest` endpoint has no built-in rate limiting. For production deployments, use an nginx reverse proxy with `limit_req_zone` (see README.md for example config) or add middleware-based rate limiting.

## rsyslog batch inserter (omprog)

For high-volume syslog ingestion, replace rsyslog's `ompgsql` with a custom batch inserter using `omprog`. This would buffer messages and use multi-row INSERT or COPY protocol for better throughput.

See `docs/batch-inserter-design.md` for the full proposal.
