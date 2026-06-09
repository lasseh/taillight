# `internal/analyzer` — AI log analysis pipeline

Turns a window of syslog events into a structured markdown briefing using a
local LLM (Ollama). One run = one report.

The core idea: **we never hand raw log lines to the model.** Postgres does the
heavy lifting first — grouping thousands of events into a few dozen ranked,
domain-aware aggregates — and the LLM only narrates that compact summary. This
keeps prompts small (~4–6K tokens against an 8192 window), keeps the numbers
exact, and stops the model from hallucinating over a wall of text.

## The flow

```
RunParams{Feed, Hosts, Period, Mode}
        │
        ▼
  Analyzer.Run                                       (run.go)
        │
        ├─ client.Ping            Ollama reachable? fail fast if not
        │
        ├─ gather ───────────────► Postgres aggregates              (gather.go)
        │      • TopMsgIDs (≤25)        grouped by MSGID / msg_pattern
        │      • SeverityComparison     current/day vs 7-day baseline
        │      • TopErrorHosts (≤15)    all-hosts runs only
        │      • EventClusters (5-min)  all-hosts runs only
        │      • NewMsgIDs              signatures unseen in prior 7 days
        │      • TopPrograms/Facilities srvlog only
        │      • VolumeTimeline         → unicode sparkline + peaks
        │      • MsgIDSamples           2 samples/top sig, 1/new sig, 300-char cap
        │      • JuniperRefs            netlog only: cause/action lookup
        │
        ├─ isEmptyData? ─── yes ─► deterministic "quiet window" stub, skip LLM
        │
        ├─ buildPrompt ──────────► render system.md + user.md templates (prompt.go)
        │                          (+ scoped anti-hallucination guard in code)
        │
        ├─ client.Chat ──────────► Ollama (llama3.1:8b, temp 0.3, num_ctx 8192)
        │
        ├─ validateReport ─── bad ─► one corrective retry, keep best   (structure.go)
        │
        └─ prependReportHeader ──► Result{Report, PromptTokens, CompletionTokens}
```

## Why aggregate instead of dumping raw logs

A naïve pipeline streams raw log text into the prompt and hopes the model
summarizes it. That burns tokens, blows the context window on busy days, and
invites the model to invent trends. Generic "context compressors" try to undo
that damage with lossy text compression after the fact.

We sidestep the whole problem: the *database* is the compressor, and it's
domain-aware. `GetTopMsgIDs` collapses thousands of repetitions into one ranked
signature with exact counts and a per-severity histogram. `GetSeverityComparison`
turns volume into a baseline-relative percentage. Sparklines encode a whole
timeline in ~24 characters. The only verbatim log text that survives is ~50
short sample lines, attached so the model can actually read *what* an event said.

Result: the summary is lossless on the things that matter (counts, severities,
percentages, hostnames) and small enough that we stay well under the context
window — no second compression pass, no extra dependency.

## Key design decisions

- **Scope-aware gathering** (`gather.go`). A run is either all-hosts or scoped
  to an explicit host set. Scoped runs skip "Top Error Hosts" and "Event
  Clusters" (tautological / degraded when you've already picked the hosts) and
  prepend a hard anti-"across-the-fleet" guard to the system prompt
  (`scopedGuardSystemPreamble`, in code so a prompt edit can't drop it).

- **Rate-normalized baselines** (`run.go`, ~line 220). Current-window severity
  counts are divided to a per-day rate so a 1-hour incident window compares
  apples-to-apples with the always-daily 7-day baseline. Otherwise a 5× spike in
  the last hour would read as "quieter than baseline".

- **Empty-window short-circuit** (`isEmptyData`). If nothing happened, we return
  deterministic text and never call the LLM — asking a model to narrate the
  absence of data just produces invented upticks. The persisted row is
  `completed` with `0/0` tokens.

- **Structure validation + one retry** (`structure.go`). Each mode must emit an
  exact set of H2 headers in order (no "Recommendations"/"Appendix" padding) and
  a bolded status/trend/verdict token in the first section. On violation we send
  one corrective follow-up and keep whichever reply validates — we never make
  the report worse.

- **Hot-reloadable prompts** (`prompt.go`). System/user templates live in
  `prompts/<mode>/{system,user}.md`, embedded by default but overridable via
  `analysis.prompts_dir`. Files are re-read on every run, so prompt edits take
  effect without a rebuild or restart.

## Prompt modes

| Mode       | Use                  | First-section token                          |
|------------|----------------------|----------------------------------------------|
| `daily`    | default, 24h review  | `**Status: NOMINAL\|WATCH\|ACT NOW**`        |
| `weekly`   | trend review         | `**Trend: IMPROVING\|STEADY\|DEGRADING\|MIXED**` |
| `incident` | narrow manual triage | `**STAND DOWN\|INVESTIGATE\|CONTAIN\|ESCALATE**` |

Required section sets per mode live in `requiredHeaders` (`structure.go`).

## Configuration (`config.yml` → `analysis:`)

```yaml
analysis:
  enabled: true
  ollama_url: "http://localhost:11434"
  model: "llama3.1:8b"     # also tried: mixtral:8x7b, llama3.1:70b
  temperature: 0.3         # low = factual/deterministic
  num_ctx: 8192            # context window (tokens)
  prompts_dir: ""          # empty = embedded defaults; set to override + hot-reload
```

## Boundaries

- **Input feeds:** `srvlog`, `netlog`, or `all`. Not `applog`.
- **`Run` is pure compute + inference** — it returns a `Result`. Persistence,
  queueing, and timeouts are the worker's job (`internal/worker/analysis.go`);
  HTTP wiring is `setupAnalysis` in `serve.go`.
- **`Store` is a consumer-side interface** (`analyzer.go`); the concrete queries
  live in `internal/postgres/analysis_store.go`.

## Files

| File           | Responsibility                                              |
|----------------|-------------------------------------------------------------|
| `analyzer.go`  | `Analyzer`, `Config`, `RunParams`, `Result`, `Store` iface  |
| `run.go`       | orchestration: gather → prompt → infer → validate           |
| `gather.go`    | Postgres aggregation, sparklines, peak extraction, caps     |
| `prompt.go`    | template load/parse/render, scope label, hot-reload         |
| `structure.go` | output validation (headers + first-section) and retry text  |
| `header.go`    | deterministic report header (title + date block)            |
| `prompts/`     | per-mode `system.md` / `user.md` templates                  |
```
