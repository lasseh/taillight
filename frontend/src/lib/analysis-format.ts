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
    case 'all':
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
  all: 'Combined',
}

export function reportTitle(r: Pick<AnalysisReportSummary, 'feed' | 'prompt_mode'>): string {
  const feed = feedLabel[r.feed] ?? r.feed
  switch (r.prompt_mode) {
    case 'daily':
      return `${feed} daily brief`
    case 'weekly':
      return `${feed} weekly review`
    case 'incident':
      return `${feed} incident triage`
    default:
      return `${feed} report`
  }
}

// briefingTitle is the long-form heading for the report detail page — paired
// with formatPeriodRange and formatPeriodUTC to render the classic ops-brief
// header. The list view stays on the shorter reportTitle() above.
const briefingLabel: Record<AnalysisPromptMode, string> = {
  daily: 'Daily Operations Briefing',
  weekly: 'Weekly Operations Briefing',
  incident: 'Incident Briefing',
}

export function briefingTitle(mode: AnalysisPromptMode | undefined): string {
  return (mode && briefingLabel[mode]) || 'Operations Briefing'
}

// formatPeriodRange renders the date span used in the briefing heading:
//   "2026-05-20 → 2026-05-21".
// Uses UTC so a report's calendar dates are stable across the operator's
// local timezone — the briefing is anchored to the syslog period, not their
// laptop clock.
export function formatPeriodRange(start: string, end: string): string {
  return `${formatDateUTC(start)} → ${formatDateUTC(end)}`
}

// formatPeriodUTC renders the precise window for the sub-line:
//   "2026-05-20 19:19 UTC – 2026-05-21 19:19 UTC".
export function formatPeriodUTC(start: string, end: string): string {
  return `${formatDateTimeUTC(start)} – ${formatDateTimeUTC(end)}`
}

function formatDateUTC(ts: string): string {
  const d = new Date(ts)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())}`
}

function formatDateTimeUTC(ts: string): string {
  const d = new Date(ts)
  const pad = (n: number) => String(n).padStart(2, '0')
  return (
    `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())} ` +
    `${pad(d.getUTCHours())}:${pad(d.getUTCMinutes())} UTC`
  )
}

export function formatDuration(r: AnalysisReport | AnalysisReportSummary): string {
  if (!r.started_at || !r.completed_at) return ''
  const ms = new Date(r.completed_at).getTime() - new Date(r.started_at).getTime()
  if (ms < 1000) return `${ms}ms`
  const seconds = Math.round(ms / 1000)
  if (seconds < 60) return `${seconds}s`
  return `${Math.floor(seconds / 60)}m ${seconds % 60}s`
}
