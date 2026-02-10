# Alert Aggregation / Correlation

## Overview

Taillight's notification engine currently aggregates events per rule via a burst window, but all events matching a rule are grouped into a single count regardless of source. When a network incident causes 50 routers to report BGP neighbor down simultaneously, the operator receives one notification saying "50 events matched rule X" — losing the critical detail of *which* devices are affected and whether the events are related.

Alert correlation extends the burst watcher to group events by a sub-key (hostname, service, or custom field), apply per-group thresholds, and produce richer notification payloads. This transforms noisy event floods into actionable incident summaries.

## Current State

### Burst watcher

`burstwatcher.go` groups events by `ruleID` only:

```go
// burstwatcher.go:18-23
type BurstWatcher struct {
    mu            sync.Mutex
    bursts        map[int64]*burst  // keyed by ruleID
    onFlush       func(ruleID int64, first Payload, count int)
    defaultWindow time.Duration
}
```

When `Add()` is called (`burstwatcher.go:36-58`), it either starts a new burst window for the rule or increments the existing count. On flush, it calls `onFlush` with the first event and total count — no grouping information.

### Engine rule matching

`engine.go:136-161` (`HandleSyslogEvent`) iterates all rules, checks if the event matches, builds a `Payload` with the single event, and calls `bursts.Add(r.ID, window, payload)`.

`engine.go:164-191` (`HandleAppLogEvent`) does the same for applog events.

### Burst flush → dispatch

`engine.go:231-274` (`onBurstFlush`) receives `(ruleID, firstPayload, count)`. It checks cooldown, resolves channels, and enqueues a `dispatchJob`. The payload carries `EventCount` and a single representative event — there's no list of affected hosts or group breakdown.

### Cooldown tracker

`cooldown.go` manages per-rule cooldowns. After a notification fires, the rule enters cooldown and suppresses subsequent bursts. It tracks suppression count but not group-level detail.

### Rule schema

`migrations/000003_notifications.up.sql:13-38` defines `notification_rules` with filter fields and burst/cooldown config:

```sql
burst_window     INTEGER NOT NULL DEFAULT 30,
cooldown_seconds INTEGER NOT NULL DEFAULT 300,
```

No correlation or grouping configuration exists.

## Proposed Design

### Correlation model

Each rule gains optional correlation config:

- **group_by** — The field to sub-group events by (e.g., `hostname`, `service`, `programname`, or a custom key).
- **correlation_window** — Time window for grouping related events (may differ from burst_window).
- **threshold** — Minimum number of events in a group before it fires a notification.

Example: rule "BGP errors on core routers" with `group_by: hostname`, `correlation_window: 30s`, `threshold: 1`. When 5 routers each report BGP down within 30s, the notification includes a summary: "5 devices affected: core-01, core-02, core-03, core-04, core-05."

### Schema changes

```sql
-- Add to notification_rules
ALTER TABLE notification_rules
    ADD COLUMN group_by TEXT CHECK (group_by IN ('hostname','programname','service','component','host')),
    ADD COLUMN correlation_window INTEGER,  -- seconds, NULL = use burst_window
    ADD COLUMN correlation_threshold INTEGER NOT NULL DEFAULT 1;
```

### Burst watcher redesign

Change the burst key from `ruleID` to `(ruleID, groupKey)`:

```go
type burstKey struct {
    RuleID   int64
    GroupVal string  // e.g., hostname value; empty if no group_by
}

type BurstWatcher struct {
    mu      sync.Mutex
    bursts  map[burstKey]*burst
    groups  map[int64]*correlationGroup  // ruleID → group accumulator
    onFlush func(ruleID int64, summary CorrelationSummary)
}

type CorrelationSummary struct {
    First      Payload
    EventCount int
    Groups     map[string]int  // groupVal → count
}
```

Flow:

1. Event matches rule with `group_by: hostname`.
2. Extract group value: `event.Hostname` → `"core-01"`.
3. `bursts.Add(burstKey{ruleID, "core-01"}, window, payload)` — per-host burst.
4. Simultaneously, accumulate into `groups[ruleID]` to track which hosts are affected.
5. When the correlation window expires, flush the group accumulator to produce a summary.

### Notification payload changes

Extend `Payload` to carry group information:

```go
type Payload struct {
    Kind        EventKind
    RuleName    string
    Timestamp   time.Time
    EventCount  int
    Groups      map[string]int  // hostname → count; nil if no correlation
    SyslogEvent *model.SyslogEvent
    AppLogEvent *model.AppLogEvent
}
```

Notification backends (Slack, webhook) render this as a summary table:

```
Rule: BGP errors on core routers
Events: 47 in 30s window
Affected devices (5):
  core-01: 12 events
  core-02: 10 events
  core-03: 9 events
  core-04: 8 events
  core-05: 8 events
```

### Backward compatibility

Rules without `group_by` set behave exactly as today — the burst watcher uses an empty string group key, and the correlation group is nil. No changes to existing behavior.

## Implementation Notes

### Files to modify

| File | Change |
|------|--------|
| `internal/notification/burstwatcher.go` | Composite key, group accumulation, correlation flush |
| `internal/notification/engine.go` | Extract group value from event, pass to burst watcher |
| `internal/notification/types.go` | Add `CorrelationSummary`, update `Payload` |
| `internal/notification/cooldown.go` | No change (cooldown stays per-rule, not per-group) |
| `internal/notification/rule.go` | Add `GroupBy`, `CorrelationWindow`, `CorrelationThreshold` to `Rule` |
| `internal/notification/store.go` | Update rule scan to include new columns |
| `internal/notification/slack.go` | Render group summary in Slack message |
| `internal/notification/webhook.go` | Include groups in webhook JSON payload |
| `migrations/000004_alert_correlation.up.sql` | Schema changes |
| Frontend notification rule editor | Add group_by/window/threshold fields |

### Key complexity: two-tier windowing

The main design challenge is the relationship between per-group burst windows and the overall correlation window:

- **Per-group burst**: "collect events from core-01 for 30s" (existing burst watcher behavior, now per group).
- **Correlation window**: "after first group fires, wait 30s more to see if other hosts are also affected before sending."

Option A: Single window — burst window applies to each group independently, and the correlation summary is just whatever groups fired within the same cooldown period. Simpler but less precise.

Option B: Two-tier — burst window per group, then a separate correlation window that collects fired groups before producing the summary. More accurate but adds complexity.

Recommendation: Start with Option A. The cooldown period already provides a natural "collection window" for related events. This avoids a major rewrite of the burst watcher.

### Group value extraction

```go
func extractGroupValue(rule Rule, event model.SyslogEvent) string {
    switch rule.GroupBy {
    case "hostname":
        return event.Hostname
    case "programname":
        return event.Programname
    default:
        return ""
    }
}
```

## Open Questions

1. **Two-tier vs. single window?** Option A (cooldown-based grouping) is simpler. Option B (explicit correlation window) is more precise. Which is acceptable for v1?
2. **Max groups per rule?** In a large network, a storm could produce thousands of unique hostnames. Cap the group map at a configurable limit (e.g., 100) and summarize the rest as "and N more."
3. **Group-by multiple fields?** `group_by: "hostname,programname"` would produce composite keys. Useful but complicates the API. Start with single field.
4. **Should correlation threshold work across groups?** E.g., "only notify if 3+ distinct hosts are affected" vs. "only notify if a single host has 3+ events." The former is more interesting for incident detection.
