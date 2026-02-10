# Saved Filter Presets

## Overview

Operators frequently use the same filter combinations when investigating issues — e.g., "all errors from core routers" or "BGP events from edge switches." Currently, these filters must be re-entered manually or bookmarked as URLs. Saved filter presets let users name, store, and quickly recall filter combinations, reducing friction for repetitive investigations.

This is a high-value feature because it directly improves daily workflow for operators who monitor specific device groups or failure patterns.

## Current State

### Filter store factory

The frontend uses a generic `createFilterStore` factory (`frontend/src/stores/filter-store-factory.ts`) that creates Pinia stores with automatic URL sync:

```typescript
// filter-store-factory.ts:14-74
export function createFilterStore<K extends string>(
  id: string,
  filterKeys: readonly K[],
  routeName: string,
)
```

Each store holds a reactive `filters` object (all string values), syncs to URL query params via `watch`, and reads initial state from URL on mount via `initFromURL()`.

### Syslog filters

`frontend/src/stores/syslog-filters.ts` defines keys: `hostname`, `programname`, `syslogtag`, `facility`, `severity_max`, `search`.

### Applog filters

`frontend/src/stores/applog-filters.ts` defines keys: `service`, `component`, `host`, `level`, `search`.

### Backend filter parsing

`model/syslog.go:231-310` (`ParseSyslogFilter`) and `model/applog.go:102-152` (`ParseAppLogFilter`) parse HTTP query params into filter structs. The saved presets would store the same key-value pairs that these parsers consume.

## Proposed Design

### Data model

```sql
-- New migration: 000004_filter_presets.up.sql
CREATE TABLE filter_presets (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    UUID REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 100),
    event_kind TEXT NOT NULL CHECK (event_kind IN ('syslog', 'applog')),
    filters    JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, name, event_kind)
);

CREATE INDEX idx_filter_presets_user ON filter_presets (user_id, event_kind);
```

The `filters` JSONB column stores the filter key-value pairs directly, matching the query parameter names:

```json
{
  "hostname": "core-*",
  "severity_max": "3",
  "search": "BGP"
}
```

This avoids schema changes when filter fields are added — the JSONB blob is opaque to the database.

### API surface

```
GET    /api/v1/filter-presets?event_kind=syslog   — list user's presets
POST   /api/v1/filter-presets                      — create preset
PUT    /api/v1/filter-presets/{id}                 — update preset
DELETE /api/v1/filter-presets/{id}                 — delete preset
```

#### Create request

```json
{
  "name": "Core router errors",
  "event_kind": "syslog",
  "filters": {
    "hostname": "core-*",
    "severity_max": "3"
  }
}
```

#### List response

```json
{
  "data": [
    {
      "id": 1,
      "name": "Core router errors",
      "event_kind": "syslog",
      "filters": { "hostname": "core-*", "severity_max": "3" },
      "created_at": "2025-01-15T10:00:00Z"
    }
  ]
}
```

### Backend handler

A new `FilterPresetHandler` in `internal/handler/` with standard CRUD methods. The handler validates:

- `event_kind` is valid (`syslog` or `applog`)
- `filters` keys are a subset of allowed filter keys for that event kind
- `name` is non-empty and within length limits
- User owns the preset on update/delete

### Frontend integration

The filter store factory gains preset awareness:

1. **Preset dropdown** — A select/dropdown component in the filter bar that lists the user's presets for the current event kind.
2. **Load preset** — Selecting a preset calls `clearAll()` then sets each filter value from the preset's `filters` object. The existing URL sync (`syncToURL`) automatically updates the URL.
3. **Save current filters** — A "Save" button that POSTs the current `activeFilters` as a new preset.
4. **Delete preset** — A delete button on each preset entry.

The key integration point is `filter-store-factory.ts:40-44` (`clearAll`) and the reactive `filters` object — loading a preset is just assigning values to the existing reactive state.

## Implementation Notes

### Files to create

| File | Purpose |
|------|---------|
| `migrations/000004_filter_presets.up.sql` | Table schema |
| `migrations/000004_filter_presets.down.sql` | Drop table |
| `internal/postgres/filter_preset_store.go` | CRUD store methods |
| `internal/handler/filter_preset.go` | HTTP handler |
| `internal/model/filter_preset.go` | `FilterPreset` struct |

### Files to modify

| File | Change |
|------|--------|
| `cmd/taillight/serve.go` | Wire handler, register routes |
| `frontend/src/stores/filter-store-factory.ts` | Add preset load/save methods |
| `frontend/src/stores/syslog-filters.ts` | No change (factory handles it) |
| `frontend/src/stores/applog-filters.ts` | No change (factory handles it) |
| Frontend filter bar components | Add preset dropdown UI |

### Validation approach

When saving a preset, validate filter keys against an allowlist per event kind:

```go
var allowedSyslogFilterKeys = map[string]struct{}{
    "hostname": {}, "programname": {}, "syslogtag": {},
    "facility": {}, "severity_max": {}, "search": {},
    "fromhost_ip": {}, "severity": {}, "msgid": {},
}
```

This prevents arbitrary data in the JSONB column while staying forward-compatible — adding a new filter field just means updating the allowlist.

## Open Questions

1. **Shared presets?** Should presets be per-user only, or support team-shared presets (e.g., `user_id IS NULL` for global presets)?
2. **Preset ordering?** Should users be able to reorder presets, or is alphabetical/most-recent sufficient?
3. **Maximum presets per user?** A reasonable cap (e.g., 50) prevents abuse without limiting normal usage.
4. **Anonymous mode?** When auth is disabled (`AllowAnonymous` middleware), should presets use localStorage instead of the database?
