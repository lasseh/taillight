# Device Health / Silence Detection

## Overview

Taillight ingests syslog events from network devices continuously, but there is no mechanism to detect when a device *stops* sending. A router that goes silent is often more critical than one that sends error messages — it may be unreachable, crashed, or partitioned.

This feature adds device health tracking by monitoring last-seen timestamps per hostname and generating alerts when a device exceeds a configurable silence window. It reuses the existing `syslog_meta_cache` infrastructure and integrates with the notification engine as a new rule type.

## Current State

### Meta cache (already populated)

The `syslog_meta_cache` table is populated by a trigger on every INSERT into `syslog_events`. It stores distinct `(column_name, value)` pairs for hostname, programname, and syslogtag:

```sql
-- migrations/000001_init_schema.up.sql:174-196
CREATE TABLE IF NOT EXISTS syslog_meta_cache (
    column_name TEXT NOT NULL,
    value       TEXT NOT NULL,
    PRIMARY KEY (column_name, value)
);
```

The trigger `trg_syslog_meta_cache` fires `cache_syslog_meta()` on every insert, upserting hostname/programname/syslogtag via `ON CONFLICT DO NOTHING`. However, it does **not** track timestamps — only existence.

### Meta queries in store

`store.go:142-195` provides `ListHosts()`, `ListPrograms()`, `ListTags()`, and `ListFacilities()` — all read from the cache tables. These return string/int slices but no timing data.

### Notification engine

`engine.go:136-191` evaluates rules against incoming syslog/applog events via `HandleSyslogEvent` and `HandleAppLogEvent`. Rules are filter-based and only fire on *matching events*, not on the *absence* of events.

## Proposed Design

### Data model

Add a `last_seen_at` column to `syslog_meta_cache` for hostname rows, updated on every insert:

```sql
-- New migration: 000004_device_health.up.sql
ALTER TABLE syslog_meta_cache ADD COLUMN last_seen_at TIMESTAMPTZ;

-- Update the trigger to set last_seen_at for hostname rows
CREATE OR REPLACE FUNCTION cache_syslog_meta()
RETURNS trigger AS $$
BEGIN
    INSERT INTO syslog_meta_cache (column_name, value, last_seen_at)
    VALUES
        ('hostname', NEW.hostname, now()),
        ('programname', NEW.programname, NULL),
        ('syslogtag', NEW.syslogtag, NULL)
    ON CONFLICT (column_name, value) DO UPDATE
        SET last_seen_at = CASE
            WHEN EXCLUDED.column_name = 'hostname' THEN now()
            ELSE syslog_meta_cache.last_seen_at
        END;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
```

Alternative: a dedicated `device_health` table. The meta cache approach is simpler since the trigger already exists, but a separate table avoids overloading the cache and allows per-device configuration (custom silence thresholds).

### Health check goroutine

A periodic goroutine in the notification engine (or a standalone component) queries for silent devices:

```go
// Runs every check interval (e.g., 60s)
func (e *Engine) checkDeviceHealth(ctx context.Context) {
    threshold := time.Now().Add(-e.cfg.SilenceWindow) // e.g., 10 minutes
    silent, err := e.store.ListSilentDevices(ctx, threshold)
    // For each silent device, evaluate silence rules and fire notifications
}
```

### New store method

```go
// ListSilentDevices returns hostnames whose last_seen_at is older than the threshold.
func (s *Store) ListSilentDevices(ctx context.Context, threshold time.Time) ([]DeviceHealth, error)

// ListDeviceHealth returns all tracked devices with their last-seen time and status.
func (s *Store) ListDeviceHealth(ctx context.Context) ([]DeviceHealth, error)
```

### New rule type: silence alert

Extend the `notification_rules` table with a `rule_type` column (or use a new `event_kind` value like `"silence"`):

```sql
ALTER TABLE notification_rules ADD COLUMN rule_type TEXT NOT NULL DEFAULT 'match'
    CHECK (rule_type IN ('match', 'silence'));
-- silence rules use hostname pattern + silence_window fields
ALTER TABLE notification_rules ADD COLUMN silence_window_seconds INTEGER;
```

### API surface

```
GET /api/v1/meta/health
```

Response:

```json
{
  "data": [
    {
      "hostname": "core-rtr-01",
      "last_seen_at": "2025-01-15T10:30:00Z",
      "status": "healthy",
      "silence_seconds": 120
    },
    {
      "hostname": "edge-sw-03",
      "last_seen_at": "2025-01-15T08:00:00Z",
      "status": "silent",
      "silence_seconds": 9120
    }
  ]
}
```

Status is computed: `healthy` if `last_seen_at` is within the default silence window, `silent` otherwise.

### Frontend

Add health status indicators to the existing hosts list (from `ListHosts`). Could be as simple as a colored dot (green/red) next to each hostname on the dashboard's top-hosts list.

## Implementation Notes

### Files to modify

| File | Change |
|------|--------|
| `migrations/000004_device_health.up.sql` | Add `last_seen_at` column, update trigger |
| `internal/postgres/store.go` | Add `ListDeviceHealth`, `ListSilentDevices` methods |
| `internal/model/syslog.go` | Add `DeviceHealth` struct |
| `internal/handler/syslog.go` | Add `GET /meta/health` endpoint |
| `internal/notification/engine.go` | Add `checkDeviceHealth` goroutine in `Start()` |
| `internal/notification/types.go` | Add silence rule type constants |
| `internal/notification/store.go` | Extend store interface for silence rules |
| `frontend/` | Health indicators on hosts list |

### Key considerations

- The `ON CONFLICT DO UPDATE` change to the meta cache trigger adds a write on every insert (currently `DO NOTHING`). For high-throughput syslog, this means every row does an UPDATE to set `last_seen_at`. This is acceptable because the meta cache table is small (bounded by distinct hostnames), but should be benchmarked.
- The health check goroutine needs deduplication — don't fire silence alerts repeatedly for the same device. The existing `CooldownTracker` (`cooldown.go`) can be reused for this.
- Consider a configurable per-device silence window (some devices send infrequently by design).

## Open Questions

1. **Separate table vs. meta cache extension?** Extending `syslog_meta_cache` is simpler but couples health tracking to the meta cache's `ON CONFLICT` behavior. A dedicated `device_health` table is cleaner and allows per-device config.
2. **Default silence window?** 5 minutes? 10 minutes? Should this be a global config value or per-rule?
3. **Should silence alerts auto-resolve?** When a device comes back online, should a "recovery" notification be sent?
4. **Applog coverage?** Should this also track applog services going silent, or syslog devices only?
