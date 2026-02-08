# Analysis Feature — Removal Guide

This document lists every file and modification introduced by the AI morning analysis feature, so it can be cleanly removed.

## New files (delete entirely)

```
api/internal/model/analysis.go
api/internal/ollama/client.go
api/internal/analyzer/analyzer.go
api/internal/analyzer/gather.go
api/internal/analyzer/prompt.go
api/internal/analyzer/run.go
api/internal/scheduler/scheduler.go
api/internal/postgres/analysis_store.go
api/internal/handler/analysis.go
api/migrations/000002_analysis_reports.up.sql
api/migrations/000002_analysis_reports.down.sql
```

After deleting, remove the empty directories:

```
api/internal/analyzer/
api/internal/ollama/
api/internal/scheduler/
```

## Modified files

### `api/internal/config/config.go`

Remove the `AnalysisConfig` struct and the `Analysis` field from `Config`. Remove the six `v.SetDefault("analysis.*", ...)` lines and the `Analysis: AnalysisConfig{...}` block in `Load()`.

### `api/internal/metrics/metrics.go`

Remove the two variables at the bottom of the `var` block:

- `AnalysisRunsTotal`
- `AnalysisDurationSeconds`

### `api/cmd/taillight/serve.go`

Remove these imports:

- `"github.com/lasseh/taillight/internal/analyzer"`
- `"github.com/lasseh/taillight/internal/ollama"`
- `"github.com/lasseh/taillight/internal/scheduler"`

Remove the `// Analysis (optional).` block that creates `analysisHandler`, the ollama client, the analyzer, and starts the scheduler goroutine.

Remove the `// Analysis endpoints.` route group inside `r.Route("/api/v1", ...)`.

### `api/config.yaml.example`

Remove the `# AI-powered daily syslog analysis...` comment block and the `analysis:` YAML section at the bottom.

## Database

Run the down migration or manually:

```sql
DROP TABLE IF EXISTS analysis_reports;
```

If using golang-migrate, the migration number is `000002`.
