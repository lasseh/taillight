# Search Query Language

## Overview

Taillight's current filtering is field-based: each filter parameter is a separate URL query param (`hostname=core-*&severity_max=3&search=BGP`). This works well for simple queries but becomes cumbersome for complex investigations that combine multiple conditions with different operators.

A search query language adds a single `q` parameter that accepts a structured query string (e.g., `hostname:core-* AND severity:<=3 AND message:BGP`). The parser translates this into the existing `SyslogFilter`/`AppLogFilter` structs, reusing all existing SQL generation — no changes to the store layer.

## Current State

### Filter structs

`model/syslog.go:105-117` defines `SyslogFilter` with fields for hostname, IP, programname, severity, facility, tag, msgid, search, and time range. `model/applog.go:64-73` defines `AppLogFilter` with service, component, host, level, search, and time range.

### Filter parsing

`ParseSyslogFilter` (`model/syslog.go:231-310`) reads individual query params and populates the struct. `ParseAppLogFilter` (`model/applog.go:102-152`) does the same for applog.

### SQL generation

`applySyslogFilter` (`store.go:197-238`) builds WHERE clauses from a `SyslogFilter`. It handles wildcard hostnames (ILIKE), exact matches, severity ranges, and ILIKE substring search on messages.

### Trigram index

The message column has a trigram GIN index (`migrations/000001_init_schema.up.sql:60-61`):

```sql
CREATE INDEX IF NOT EXISTS idx_syslog_message_trgm
    ON syslog_events USING GIN (message gin_trgm_ops);
```

This index is currently used only for `message ILIKE '%search%'` queries via the `search` filter field. A query language could expose more targeted message searches.

### In-memory matching

`SyslogFilter.Matches()` (`model/syslog.go:164-193`) and `AppLogFilter.Matches()` (`model/applog.go:78-99`) filter SSE events client-side. A query language parser must produce the same filter structs so SSE filtering works identically.

## Proposed Design

### Query syntax

```
hostname:core-* AND severity:<=3 AND program:rpd
service:myapp AND level:>=WARN AND message:"connection refused"
hostname:edge-* OR hostname:core-*
NOT hostname:test-*
```

#### Field mapping

| Query field | SyslogFilter field | AppLogFilter field |
|------------|-------------------|-------------------|
| `hostname` | `Hostname` | — |
| `ip` | `FromhostIP` | — |
| `program` | `Programname` | — |
| `severity` | `Severity` / `SeverityMax` | — |
| `facility` | `Facility` | — |
| `tag` | `SyslogTag` | — |
| `msgid` | `MsgID` | — |
| `service` | — | `Service` |
| `component` | — | `Component` |
| `host` | — | `Host` |
| `level` | — | `Level` |
| `message` | `Search` | `Search` |

#### Operators

- Exact match: `hostname:router01`
- Wildcard: `hostname:core-*`
- Comparison (numeric/severity): `severity:<=3`, `level:>=WARN`
- Quoted phrases: `message:"link down"`
- Boolean: `AND`, `OR`, `NOT` (AND is implicit between terms)

### Parser architecture

```
query string → lexer → tokens → parser → AST → filter struct
```

The parser lives in `internal/model/` as a new file (e.g., `query.go`). It produces the existing filter structs:

```go
// ParseQuery parses a query string into a SyslogFilter.
func ParseSyslogQuery(q string) (SyslogFilter, error)

// ParseAppLogQuery parses a query string into an AppLogFilter.
func ParseAppLogQuery(q string) (AppLogFilter, error)
```

### Integration with existing params

The `q` parameter works alongside individual filter params. Individual params take precedence (they override fields set by `q`). This allows gradual adoption — existing URL-based filters continue to work.

```go
// In ParseSyslogFilter, after parsing individual params:
if q := r.URL.Query().Get("q"); q != "" {
    qf, err := ParseSyslogQuery(q)
    // Merge qf into f, individual params override
}
```

### API surface

No new endpoints. The existing list/stream endpoints gain a `q` parameter:

```
GET /api/v1/syslog?q=hostname:core-*+AND+severity:<=3
GET /api/v1/syslog/stream?q=hostname:core-*
GET /api/v1/applog?q=service:myapp+AND+level:>=WARN
```

### Frontend

- **Search bar** — A text input that accepts the query language, placed above or alongside the existing filter dropdowns.
- **Syntax highlighting** — Use a lightweight tokenizer to color field names, operators, and values differently.
- **Autocomplete** — When the user types a field name followed by `:`, suggest values from the meta cache (`frontend/src/stores/meta.ts` for syslog, `frontend/src/stores/applog-meta.ts` for applog).
- **Bidirectional sync** — When individual filters are set via dropdowns, the search bar updates to show the equivalent query string. When the search bar is used, dropdowns update to reflect the parsed fields.

## Implementation Notes

### Files to create

| File | Purpose |
|------|---------|
| `internal/model/query.go` | Lexer, parser, AST-to-filter conversion |
| `internal/model/query_test.go` | Table-driven parser tests |

### Files to modify

| File | Change |
|------|--------|
| `internal/model/syslog.go` | Merge `q` param in `ParseSyslogFilter` |
| `internal/model/applog.go` | Merge `q` param in `ParseAppLogFilter` |
| Frontend search bar component | New component |
| `frontend/src/stores/filter-store-factory.ts` | Add `q` field handling |

### Parser complexity

The parser should be hand-written (recursive descent), not generated. The grammar is simple enough that a lexer + Pratt parser covers all cases in ~200 lines. Avoid pulling in a parser generator dependency.

Key consideration: `OR` and `NOT` cannot map directly to the existing filter structs (which are AND-only). Two approaches:

1. **Restrict to AND-only** in v1 — the filter structs already represent AND conjunctions. OR/NOT support would require extending the SQL generation layer.
2. **Support OR/NOT** by extending `applySyslogFilter` to accept an AST instead of a flat struct.

Recommendation: start with AND-only. This covers 90% of use cases and avoids touching the store layer.

### Trigram index utilization

The existing trigram index on `message` supports `ILIKE '%term%'` queries. The query language's `message:` field maps directly to this. For multi-word searches (`message:"link down"`), the existing `Search` field handles this correctly.

## Open Questions

1. **AND-only vs. full boolean?** AND-only is simpler and maps directly to existing filter structs. Full boolean requires a new query execution layer.
2. **Severity comparison syntax?** `severity:<=3` (numeric) vs. `severity:<=err` (label)? Supporting both means the parser needs severity label resolution.
3. **Negation per field?** `NOT hostname:test-*` is useful but requires extending `applySyslogFilter` to support negated conditions (e.g., `WHERE hostname NOT ILIKE ...`).
4. **How to handle unknown fields?** Return an error, or silently ignore? Error is safer.
