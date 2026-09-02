import type {
  AnalysisFeed,
  AnalysisPromptMode,
  AnalysisReport,
  AnalysisReportSummary,
} from '@/types/analysis'

export function feedBadgeClass(feed: AnalysisFeed): string {
  switch (feed) {
    case 'netlog':
      return 'bg-t-blue/10 text-t-blue'
    case 'srvlog':
      return 'bg-t-green/10 text-t-green'
    case 'applog':
      return 'bg-t-purple/10 text-t-purple'
    default:
      return 'bg-t-fg-dark/10 text-t-fg-dark'
  }
}

export function promptModeBadgeClass(mode: AnalysisPromptMode | undefined): string {
  switch (mode) {
    case 'daily':
      return 'bg-t-fg-dark/10 text-t-fg-dark'
    case 'weekly':
      return 'bg-t-blue/10 text-t-blue'
    case 'incident':
      return 'bg-t-red/10 text-t-red'
    default:
      return 'bg-t-fg-dark/10 text-t-fg-dark'
  }
}

export function statusBadgeClass(status: string): string {
  switch (status) {
    case 'completed':
      return 'bg-t-green/15 text-t-green'
    case 'running':
      return 'bg-t-yellow/15 text-t-yellow'
    case 'pending':
      return 'bg-t-fg-dark/15 text-t-fg-dark'
    case 'failed':
      return 'bg-t-red/15 text-t-red'
    default:
      return 'bg-t-fg-dark/15 text-t-fg-dark'
  }
}

export function formatDate(ts: string): string {
  return new Date(ts).toLocaleString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  })
}

export function formatReportTimestamp(ts: string): string {
  const d = new Date(ts)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}.${pad(d.getMonth() + 1)}.${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function timeAgo(ts: string): string {
  const seconds = Math.floor((Date.now() - new Date(ts).getTime()) / 1000)
  if (seconds < 60) return 'just now'
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`
  const days = Math.floor(hours / 24)
  if (days < 30) return `${days}d ago`
  const months = Math.floor(days / 30)
  if (months < 12) return `${months}mo ago`
  return `${Math.floor(months / 12)}y ago`
}

const feedLabel: Record<AnalysisFeed, string> = {
  netlog: 'Netlog',
  srvlog: 'Srvlog',
  applog: 'Applog',
}

// formatScope renders a report's scope as a count phrase ("3 hosts",
// "1 service") for the title-suffix path. Empty input returns "" so callers
// can spread it after a separator without producing trailing whitespace.
// Single vs plural noun matters — a one-host scope reads as "1 host".
export function formatScope(names: string[] | undefined | null, noun = 'host'): string {
  if (!names || names.length === 0) return ''
  return `${names.length} ${names.length === 1 ? noun : noun + 's'}`
}

// scopeNames returns whichever scope list a report carries: hosts for the
// syslog feeds, services for applog. Empty when the run was fleet-wide.
export function scopeNames(r: { hosts?: string[]; services?: string[] }): string[] {
  if (r.hosts && r.hosts.length > 0) return r.hosts
  return r.services ?? []
}

export function reportTitle(
  r: Pick<AnalysisReportSummary, 'feed' | 'prompt_mode'> & {
    hosts?: string[]
    services?: string[]
  },
): string {
  const feed = feedLabel[r.feed] ?? r.feed
  let base: string
  switch (r.prompt_mode) {
    case 'daily':
      base = `${feed} daily brief`
      break
    case 'weekly':
      base = `${feed} weekly review`
      break
    case 'incident':
      base = `${feed} incident triage`
      break
    default:
      base = `${feed} report`
  }
  const scope = r.feed === 'applog' ? formatScope(r.services, 'service') : formatScope(r.hosts)
  return scope === '' ? base : `${base} · ${scope}`
}

// The briefing title and period sub-line are rendered by the backend
// (api/internal/analyzer/header.go) directly into the report markdown, so
// the frontend no longer needs helpers for them here.

export function formatDuration(r: AnalysisReport | AnalysisReportSummary): string {
  if (!r.started_at || !r.completed_at) return ''
  const ms = new Date(r.completed_at).getTime() - new Date(r.started_at).getTime()
  if (ms < 1000) return `${ms}ms`
  const seconds = Math.round(ms / 1000)
  if (seconds < 60) return `${seconds}s`
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
}
