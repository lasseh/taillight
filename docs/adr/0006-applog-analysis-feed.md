# Applog added as an analysis feed; the combined "all" feed removed

Architecture review D3 (2026-07-02) kept applog out of AI analysis: the prompts were tuned to a syslog narrative for the network team, and applog attrs are high-cardinality JSON that inflates token budgets. "all" was relabelled "All syslog", and the exclusion was recorded in the model comment, the OpenAPI notes, the TypeScript union, and the analyzer README, with "revisit on real demand".

Maintainer decision (design session 2026-09-01): the demand is real. Developers who own a service want a morning brief of what their service logged, in the style of the netlog daily. Quotes: *"now we can remove 'All' and have netlog,srvlog,applog"*, *"none of prod uses the 'All' we can remove it"*, *"we should just focus on the WARN/ERR/FATAL levels, then its not so much logs"*, and on warnings: *"they contain valuble data, so lets include them also in the analasys"*.

What changes:

- The feed enum is `netlog`, `srvlog`, `applog`. `all` is deleted from both CHECK constraints, the union source, the Juniper-ref gate, the OpenAPI enums, and the frontend picker. No production rows carry it; the migration deletes any that do.
- Applog gets its own gather and its own prompt files under `prompts/applog/daily/`, not a column mapping onto the syslog queries. The syslog user prompts hardcode the 0 to 7 severity legend, RFC 5424 MSGID, and "Top Programs", and applog's unit of interest is the service, not the host. Mapping columns would have made the model read syslog vocabulary over structured app logs.
- Raw-row queries read warn, error, and fatal only, through one case-insensitive level mapping in the model package that the summary endpoint also adopts. Volume and silent-service detection read the hourly aggregate at all levels.
- The report ranks services rather than enumerating them (roughly 300 in production), scoped by a services list on the create form. Caps are config under `analysis.applog`, with defaults sized for the 32k window of the production Ollama box.
- Applog is daily-only. Weekly, monthly, and incident are rejected for it until the daily prompt has been tuned on real data.

Not changed: the run, worker, scheduler, and report layers; the syslog prompt files and the `prompts_dir` override; the single global Ollama endpoint (a per-feed override is a filed follow-up). Code-aware analysis and any service-to-repository mapping were considered and dropped.

Reopen the column-mapping approach only if the applog prompt converges on the syslog data block. At design time it did not.

See `.scratch/applog-ai-analysis/PRD.md` and `.scratch/notes-triage-2026-07/issues/09-applog-ai-analysis.md`.
