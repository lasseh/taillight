# Juniper syslog reference data

XLSX files in this directory are auto-imported into the `juniper_netlog_ref`
table on `taillight serve` startup. They power `GET /api/v1/juniper/lookup`
and the message-catalog enrichment in the UI.

## How it works

- On startup, every `*.xlsx` file in this folder is considered.
- The target OS is inferred from the filename:
  - filename contains `evolved` (case-insensitive) → `junos-evolved`
  - otherwise → `junos`
- A file is **skipped** when `juniper_netlog_ref` already has at least one
  row for its OS. To force a re-import, either:
  - `TRUNCATE juniper_netlog_ref` (full reset, both OSes), or
  - `taillight import --file <path> --os junos|junos-evolved` (per file).
- Files starting with `~$` (Excel lockfiles) and `.` (hidden) are ignored.

## Where to get the files

Juniper publishes the XLSX message catalogs alongside each Junos OS release at
[Juniper TechLibrary → System Log Messages](https://www.juniper.net/documentation/).
Drop the latest version here and rebuild — naming is free-form as long as
"evolved" appears in the Junos OS Evolved file.

## Disabling

Set `juniper_ref_path: ""` in `config.yml` to skip auto-import entirely.
The manual `taillight import` CLI continues to work either way.
