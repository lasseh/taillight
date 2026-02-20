<script setup lang="ts">
import { ref, computed } from 'vue'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import type { AnalysisReport } from '@/types/analysis'

const mockReports: AnalysisReport[] = [
  {
    id: 3,
    generated_at: '2026-02-20T06:00:00Z',
    model: 'llama3.1:8b',
    period_start: '2026-02-19T06:00:00Z',
    period_end: '2026-02-20T06:00:00Z',
    prompt_tokens: 4210,
    completion_tokens: 1872,
    duration_ms: 34500,
    status: 'completed',
    report: `# Daily Syslog Analysis Report
## Period: 2026-02-19 06:00 — 2026-02-20 06:00

---

## Executive Summary

The network experienced **elevated error rates** across three core switches during the reporting period. A total of **14,328 events** were processed, with **892 severity 3 (error)** and **12 severity 2 (critical)** entries detected. The primary concern is a recurring OSPF adjacency flap on \`core-sw-01\` that correlates with interface resets on the upstream link.

Overall health: **Degraded** — immediate attention recommended for the OSPF instability.

## Incident Analysis

### 1. OSPF Adjacency Flaps — core-sw-01
- **Severity:** Critical
- **Count:** 12 occurrences over 24h
- **Pattern:** Adjacency drops every ~2 hours on interface Gi0/0/1
- **Impact:** Routing convergence delays of 15-30 seconds per event
- **Related events:** \`LINEPROTO-5-UPDOWN\` on same interface

### 2. NTP Sync Loss — Multiple Hosts
- **Severity:** Warning
- **Hosts affected:** access-sw-04, access-sw-07, access-sw-12
- **Count:** 48 events
- **Root cause:** NTP server 10.0.1.5 became unreachable at 14:22 UTC
- **Resolution:** Server recovered at 16:45 UTC, all clients re-synced

### 3. Disk Space Alerts — log-collector-02
- **Severity:** Error
- **Count:** 6 events
- **Pattern:** \`/var/log\` partition at 94% capacity
- **Recommendation:** Rotate or archive logs, increase partition size

## Anomaly Detection

| Metric | Current | Baseline (7d avg) | Change |
|--------|---------|-------------------|--------|
| Total events | 14,328 | 11,450 | +25.1% |
| Error (sev 3) | 892 | 340 | +162.4% |
| Critical (sev 2) | 12 | 2 | +500.0% |
| Warning (sev 4) | 2,104 | 1,890 | +11.3% |

The **162% increase in error-level events** is directly attributable to the OSPF flapping on core-sw-01. Excluding that host, error rates are within normal bounds (+8.2%).

## Event Correlation

Three event clusters were identified with correlated timing:

1. **02:14–02:17 UTC** — \`core-sw-01\` OSPF down → \`dist-sw-03\` route recalculation → \`access-sw-04\` through \`access-sw-07\` brief connectivity loss
2. **14:22–14:25 UTC** — NTP server unreachable → 3 switches log clock skew warnings within 3 minutes
3. **19:30–19:32 UTC** — \`core-sw-01\` OSPF flap coincides with upstream provider maintenance window

## Priority Actions

1. **[CRITICAL]** Investigate physical layer on \`core-sw-01\` Gi0/0/1 — check SFP module, fiber patch, and error counters
2. **[HIGH]** Add redundant NTP server to prevent single-point-of-failure clock drift
3. **[MEDIUM]** Expand \`/var/log\` on log-collector-02 or implement aggressive log rotation
4. **[LOW]** Review OSPF hello/dead timers — consider BFD for faster failover detection`,
  },
  {
    id: 2,
    generated_at: '2026-02-19T06:00:00Z',
    model: 'llama3.1:8b',
    period_start: '2026-02-18T06:00:00Z',
    period_end: '2026-02-19T06:00:00Z',
    prompt_tokens: 3980,
    completion_tokens: 1543,
    duration_ms: 28200,
    status: 'completed',
    report: `# Daily Syslog Analysis Report
## Period: 2026-02-18 06:00 — 2026-02-19 06:00

---

## Executive Summary

A relatively quiet 24-hour period with **11,203 total events**. All metrics are within normal operating ranges. One notable event was a scheduled firmware upgrade on \`access-sw-12\` that generated expected reboot sequences.

Overall health: **Normal** — no action required.

## Incident Analysis

### 1. Scheduled Firmware Upgrade — access-sw-12
- **Severity:** Informational
- **Count:** 34 events (reboot cycle)
- **Duration:** 12 minutes downtime (02:00–02:12 UTC)
- **Status:** Upgrade completed successfully, all interfaces restored

### 2. Authentication Failures — vpn-gw-01
- **Severity:** Warning
- **Count:** 23 events
- **Pattern:** Failed RADIUS authentication attempts from external IPs
- **Assessment:** Normal brute-force noise, rate limiting is active

## Anomaly Detection

| Metric | Current | Baseline (7d avg) | Change |
|--------|---------|-------------------|--------|
| Total events | 11,203 | 11,450 | -2.2% |
| Error (sev 3) | 312 | 340 | -8.2% |
| Critical (sev 2) | 1 | 2 | -50.0% |
| Warning (sev 4) | 1,876 | 1,890 | -0.7% |

All severity levels within expected variance. No anomalies detected.

## Event Correlation

No significant cross-host event correlations detected during this period. The firmware upgrade on access-sw-12 was isolated and did not trigger cascading events.

## Priority Actions

1. **[LOW]** Review VPN authentication failure source IPs — consider geo-blocking if patterns persist
2. **[INFO]** Confirm access-sw-12 firmware version matches fleet standard`,
  },
  {
    id: 1,
    generated_at: '2026-02-18T06:00:00Z',
    model: 'llama3.1:8b',
    period_start: '2026-02-17T06:00:00Z',
    period_end: '2026-02-18T06:00:00Z',
    prompt_tokens: 4102,
    completion_tokens: 1690,
    duration_ms: 31800,
    status: 'completed',
    report: `# Daily Syslog Analysis Report
## Period: 2026-02-17 06:00 — 2026-02-18 06:00

---

## Executive Summary

Moderate activity with **12,891 events** processed. A brief power event at the secondary data center caused **UPS failover alerts** across 8 hosts. All systems recovered automatically within 4 minutes. One recurring issue: \`dist-sw-03\` continues to log high CPU warnings during peak hours.

Overall health: **Normal** — monitoring recommended for dist-sw-03 CPU.

## Incident Analysis

### 1. UPS Failover — Secondary DC
- **Severity:** Error
- **Hosts affected:** 8 devices in rack B2
- **Count:** 64 events
- **Timeline:** Power dip at 11:47 UTC, UPS engaged, utility restored at 11:51 UTC
- **Impact:** No service disruption, all devices on UPS backup

### 2. High CPU — dist-sw-03
- **Severity:** Warning
- **Count:** 18 events
- **Pattern:** CPU exceeds 85% threshold during 08:00–10:00 and 14:00–16:00 UTC
- **Root cause:** Likely ARP/MAC table churn from dense VLAN environment
- **Recurring:** This is the 5th consecutive day of this pattern

### 3. Syslog Source Unreachable — access-sw-15
- **Severity:** Warning
- **Count:** 3 events
- **Duration:** 45 minutes (03:15–04:00 UTC)
- **Assessment:** Device was unreachable for syslog forwarding; SNMP polling confirmed device was up — likely a UDP delivery issue

## Anomaly Detection

| Metric | Current | Baseline (7d avg) | Change |
|--------|---------|-------------------|--------|
| Total events | 12,891 | 11,450 | +12.6% |
| Error (sev 3) | 404 | 340 | +18.8% |
| Critical (sev 2) | 0 | 2 | -100.0% |
| Warning (sev 4) | 2,190 | 1,890 | +15.9% |

The elevated error and warning counts are attributable to the UPS failover event. Excluding rack B2 events, all metrics are within normal range.

## Event Correlation

1. **11:47–11:51 UTC** — Power event → UPS failover alerts across 8 hosts → automatic recovery. Clean correlation with utility power monitoring logs.
2. **08:00–10:00 UTC** — dist-sw-03 CPU spikes correlate with peak traffic from building A access layer.

## Priority Actions

1. **[HIGH]** Investigate dist-sw-03 CPU — profile ARP/MAC table sizes, consider enabling storm control or splitting VLANs
2. **[MEDIUM]** Review UPS capacity and test failover procedure for secondary DC rack B2
3. **[LOW]** Check syslog UDP delivery path to access-sw-15 — consider TCP syslog for reliability`,
  },
]

const selectedId = ref(mockReports[0]?.id ?? 0)

const selectedReport = computed(() =>
  mockReports.find((r) => r.id === selectedId.value),
)

const renderedMarkdown = computed(() => {
  if (!selectedReport.value) return ''
  const html = marked.parse(selectedReport.value.report) as string
  return DOMPurify.sanitize(html)
})

function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function formatDuration(ms: number): string {
  const seconds = Math.round(ms / 1000)
  if (seconds < 60) return `${seconds}s`
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
}

function formatPeriod(start: string, end: string): string {
  const s = new Date(start)
  const e = new Date(end)
  const fmt = (d: Date) =>
    d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
  return `${fmt(s)} — ${fmt(e)}`
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div class="flex-1 overflow-y-auto px-4 py-6">
      <div class="mx-auto max-w-7xl space-y-5">

        <!-- Page header -->
        <div class="flex items-center justify-between gap-4">
          <div>
            <h2 class="text-t-fg text-base font-semibold">Analysis</h2>
            <p class="text-t-fg-dark mt-1 text-sm">AI-generated syslog analysis reports</p>
          </div>

          <!-- Report selector -->
          <select
            v-model="selectedId"
            class="bg-t-bg-dark border-t-border text-t-fg rounded border px-3 py-1.5 text-xs"
          >
            <option
              v-for="r in mockReports"
              :key="r.id"
              :value="r.id"
            >
              {{ formatPeriod(r.period_start, r.period_end) }} — {{ r.model }}
            </option>
          </select>
        </div>

        <!-- Metadata bar -->
        <div
          v-if="selectedReport"
          class="bg-t-bg-dark border-t-border flex flex-wrap items-center gap-x-6 gap-y-2 rounded border px-4 py-3"
        >
          <div class="flex items-center gap-1.5">
            <span class="text-t-fg-dark text-xs">Status</span>
            <span
              class="rounded px-1.5 py-0.5 text-xs font-medium"
              :class="
                selectedReport.status === 'completed'
                  ? 'bg-t-green/15 text-t-green'
                  : selectedReport.status === 'running'
                    ? 'bg-t-yellow/15 text-t-yellow'
                    : 'bg-t-red/15 text-t-red'
              "
            >
              {{ selectedReport.status }}
            </span>
          </div>
          <div class="flex items-center gap-1.5">
            <span class="text-t-fg-dark text-xs">Model</span>
            <span class="text-t-fg text-xs font-medium">{{ selectedReport.model }}</span>
          </div>
          <div class="flex items-center gap-1.5">
            <span class="text-t-fg-dark text-xs">Period</span>
            <span class="text-t-fg text-xs font-medium">{{ formatPeriod(selectedReport.period_start, selectedReport.period_end) }}</span>
          </div>
          <div class="flex items-center gap-1.5">
            <span class="text-t-fg-dark text-xs">Tokens</span>
            <span class="text-t-fg text-xs font-medium">{{ selectedReport.prompt_tokens.toLocaleString() }} + {{ selectedReport.completion_tokens.toLocaleString() }}</span>
          </div>
          <div class="flex items-center gap-1.5">
            <span class="text-t-fg-dark text-xs">Duration</span>
            <span class="text-t-fg text-xs font-medium">{{ formatDuration(selectedReport.duration_ms) }}</span>
          </div>
          <div class="flex items-center gap-1.5">
            <span class="text-t-fg-dark text-xs">Generated</span>
            <span class="text-t-fg text-xs font-medium">{{ formatDate(selectedReport.generated_at) }}</span>
          </div>
        </div>

        <!-- Report body -->
        <div
          v-if="selectedReport"
          class="prose border-t-border rounded border px-6 py-5"
          v-html="renderedMarkdown"
        />

        <!-- Empty state -->
        <div
          v-if="!selectedReport"
          class="text-t-fg-dark flex flex-col items-center gap-2 py-20 text-sm"
        >
          <svg class="h-8 w-8 opacity-40" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
            <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
            <polyline points="14 2 14 8 20 8" />
            <line x1="16" y1="13" x2="8" y2="13" />
            <line x1="16" y1="17" x2="8" y2="17" />
            <polyline points="10 9 9 9 8 9" />
          </svg>
          <span>No analysis reports available</span>
        </div>

      </div>
    </div>
  </div>
</template>

<style scoped>
/* Theme-aware prose styling for rendered markdown */
.prose {
  color: var(--color-t-fg);
  line-height: 1.75;
}

/* ── Headings ── */
.prose :deep(h1) {
  color: var(--color-t-fg);
  font-size: 1.25rem;
  font-weight: 700;
  margin-top: 0;
  margin-bottom: 1rem;
  padding-bottom: 0.625rem;
  border-bottom: 2px solid var(--color-t-orange);
}

.prose :deep(h2) {
  color: var(--color-t-orange);
  font-size: 1rem;
  font-weight: 600;
  margin-top: 2rem;
  margin-bottom: 0.625rem;
  padding-bottom: 0.375rem;
  border-bottom: 1px solid var(--color-t-border);
}

.prose :deep(h3) {
  color: var(--color-t-teal);
  font-size: 0.875rem;
  font-weight: 600;
  margin-top: 1.5rem;
  margin-bottom: 0.375rem;
}

/* ── Body text ── */
.prose :deep(p) {
  margin-top: 0.5rem;
  margin-bottom: 0.5rem;
  font-size: 0.8125rem;
}

.prose :deep(ul),
.prose :deep(ol) {
  margin-top: 0.375rem;
  margin-bottom: 0.375rem;
  padding-left: 1.5rem;
  font-size: 0.8125rem;
}

.prose :deep(li) {
  margin-top: 0.1875rem;
  margin-bottom: 0.1875rem;
}

.prose :deep(li::marker) {
  color: var(--color-t-fg-dark);
}

/* ── Inline emphasis ── */
.prose :deep(strong) {
  color: var(--color-t-fg);
  font-weight: 600;
}

.prose :deep(em) {
  color: var(--color-t-fg-dark);
  font-style: italic;
}

/* ── Inline code ── */
.prose :deep(code) {
  color: var(--color-t-teal);
  background: var(--color-t-bg-highlight);
  padding: 0.125rem 0.375rem;
  border-radius: 0.25rem;
  font-size: 0.75rem;
  border: 1px solid var(--color-t-border);
}

/* ── Code blocks ── */
.prose :deep(pre) {
  background: var(--color-t-bg-dark);
  border: 1px solid var(--color-t-border);
  border-radius: 0.375rem;
  padding: 0.875rem 1rem;
  overflow-x: auto;
  margin-top: 0.75rem;
  margin-bottom: 0.75rem;
}

.prose :deep(pre code) {
  background: none;
  padding: 0;
  border: none;
  color: var(--color-t-fg);
}

/* ── Horizontal rules ── */
.prose :deep(hr) {
  border: none;
  border-top: 1px solid var(--color-t-border);
  margin: 1.5rem 0;
}

/* ── Tables ── */
.prose :deep(table) {
  width: 100%;
  border-collapse: collapse;
  font-size: 0.75rem;
  margin-top: 0.75rem;
  margin-bottom: 0.75rem;
  border: 1px solid var(--color-t-border);
  border-radius: 0.375rem;
  overflow: hidden;
}

.prose :deep(th) {
  color: var(--color-t-orange);
  font-weight: 600;
  text-align: left;
  padding: 0.5rem 0.75rem;
  background: var(--color-t-bg-dark);
  border-bottom: 1px solid var(--color-t-border);
}

.prose :deep(td) {
  padding: 0.375rem 0.75rem;
  border-bottom: 1px solid var(--color-t-border);
}

.prose :deep(tr:last-child td) {
  border-bottom: none;
}

.prose :deep(tr:hover td) {
  background: var(--color-t-bg-highlight);
}

/* ── Links ── */
.prose :deep(a) {
  color: var(--color-t-blue);
  text-decoration: underline;
  text-underline-offset: 2px;
}

.prose :deep(a:hover) {
  color: var(--color-t-teal);
}

/* ── Blockquotes ── */
.prose :deep(blockquote) {
  border-left: 3px solid var(--color-t-orange);
  padding-left: 1rem;
  color: var(--color-t-fg-dark);
  margin: 0.75rem 0;
  font-size: 0.8125rem;
}
</style>
