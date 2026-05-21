-- Add server-captured ingest metadata to applog_events.
--
-- source_ip  : client IP resolved by chi's middleware.RealIP at ingest time.
-- api_key_id : ID of the API key that authenticated the ingest request.
--
-- Both are populated by the ingest handler, never read from the request body,
-- so they cannot be spoofed by the shipper. Historical rows remain NULL.
-- No FK on api_key_id: api_keys rows are revoked, never hard-deleted, so
-- orphan worry is minimal and the join cost on the hot insert path isn't
-- worth it. Indexes deferred until a filter UI lands.

ALTER TABLE applog_events
    ADD COLUMN IF NOT EXISTS source_ip  INET,
    ADD COLUMN IF NOT EXISTS api_key_id UUID;
