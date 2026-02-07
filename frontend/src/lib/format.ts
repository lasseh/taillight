/** Format an ISO timestamp as HH:MM:SS (24h, en-GB). */
export function formatTime(ts: string): string {
  const d = new Date(ts)
  return d.toLocaleTimeString('en-GB', {
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  })
}

/** Format an ISO timestamp as a full date-time string. */
export function formatDateTime(ts: string): string {
  const d = new Date(ts)
  return d.toLocaleString(undefined, {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
    hour12: false,
  })
}

/** Format a key-value object as a compact string: key1=value1 key2=value2. */
export function formatAttrs(attrs: Record<string, unknown>): string {
  return Object.entries(attrs)
    .map(([k, v]) => `${k}=${typeof v === 'string' ? v : JSON.stringify(v)}`)
    .join(' ')
}

import { highlightJson } from '@/lib/highlighter'

/** Format attrs as syntax-highlighted JSON (returns HTML). */
export function highlightAttrs(attrs: Record<string, unknown> | null): string {
  return highlightJson(attrs)
}

/** Format a large number with k/M suffixes. */
export function formatNumber(n: number): string {
  if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M'
  if (n >= 1000) return (n / 1000).toFixed(1) + 'k'
  return n.toString()
}

/** Truncate a string to a maximum length, appending '…' if truncated. */
export function truncate(s: string, max: number): string {
  return s.length > max ? s.slice(0, max) + '…' : s
}
