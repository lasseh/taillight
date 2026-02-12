import { ref } from 'vue'
import { defineStore } from 'pinia'
import { api } from '@/lib/api'
import { useSyslogStream } from '@/composables/useSyslogStream'
import { useAppLogStream } from '@/composables/useAppLogStream'
import type { SyslogSummary, AppLogSummary } from '@/types/stats'
import type { SyslogEvent } from '@/types/syslog'
import type { AppLogEvent } from '@/types/applog'

const SUMMARY_REFRESH_INTERVAL = 30_000 // 30 seconds
const MAX_RECENT_EVENTS = 10
const HIGH_SEVERITY_MAX = 2 // crit and above
const HIGH_SEVERITY_LEVELS = new Set(['WARN', 'ERROR', 'FATAL'])

// Map range labels to milliseconds for computing `from` timestamps.
const rangeDurations: Record<string, number> = {
  '1h': 1 * 3600_000,
  '6h': 6 * 3600_000,
  '12h': 12 * 3600_000,
  '24h': 24 * 3600_000,
  '7d': 7 * 24 * 3600_000,
  '30d': 30 * 24 * 3600_000,
}

function rangeToFrom(range: string): string {
  const ms = rangeDurations[range] ?? 24 * 3600_000
  return new Date(Date.now() - ms).toISOString()
}

export const useHomeStore = defineStore('home', () => {
  const syslogSummary = ref<SyslogSummary | null>(null)
  const applogSummary = ref<AppLogSummary | null>(null)
  const recentSyslogEvents = ref<SyslogEvent[]>([])
  const recentApplogEvents = ref<AppLogEvent[]>([])
  const loading = ref(false)
  const loaded = ref(false)
  const error = ref<string | null>(null)
  const lastUpdated = ref<Date | null>(null)
  const range_ = ref('24h')

  let summaryTimer: ReturnType<typeof setInterval> | null = null
  let syslogUnsub: (() => void) | null = null
  let applogUnsub: (() => void) | null = null

  const syslogStream = useSyslogStream()
  const applogStream = useAppLogStream()

  function onSyslogEvent(event: SyslogEvent) {
    if (event.severity > HIGH_SEVERITY_MAX) return
    const existing = recentSyslogEvents.value
    if (existing.some(e => e.id === event.id)) return
    recentSyslogEvents.value = [...existing, event].slice(-MAX_RECENT_EVENTS)
    lastUpdated.value = new Date()
  }

  function onAppLogEvent(event: AppLogEvent) {
    if (!HIGH_SEVERITY_LEVELS.has(event.level)) return
    const existing = recentApplogEvents.value
    if (existing.some(e => e.id === event.id)) return
    recentApplogEvents.value = [...existing, event].slice(-MAX_RECENT_EVENTS)
    lastUpdated.value = new Date()
  }

  async function fetchSummaries() {
    if (!loaded.value) {
      loading.value = true
    }

    const errors: string[] = []

    // Fetch independently so one failure doesn't block the other.
    try {
      const res = await api.getSyslogSummary(range_.value)
      syslogSummary.value = res.data
    } catch (e) {
      errors.push(`syslog summary: ${e instanceof Error ? e.message : 'unknown error'}`)
    }

    try {
      const res = await api.getAppLogSummary(range_.value)
      applogSummary.value = res.data
    } catch (e) {
      errors.push(`applog summary: ${e instanceof Error ? e.message : 'unknown error'}`)
    }

    error.value = errors.length > 0 ? errors.join('; ') : null
    loading.value = false
    loaded.value = true
  }

  async function fetchInitialEvents() {
    const from = rangeToFrom(range_.value)

    try {
      const syslogEventsRes = await api.getSyslogs(
        new URLSearchParams({ severity_max: String(HIGH_SEVERITY_MAX), limit: String(MAX_RECENT_EVENTS), from }),
      )
      recentSyslogEvents.value = (syslogEventsRes.data ?? []).slice(-MAX_RECENT_EVENTS)
    } catch {
      // Non-critical, SSE will populate
    }

    try {
      const applogEventsRes = await api.getAppLogs(
        new URLSearchParams({ level: 'WARN', limit: String(MAX_RECENT_EVENTS), from }),
      )
      recentApplogEvents.value = (applogEventsRes.data ?? []).slice(-MAX_RECENT_EVENTS)
    } catch {
      // Non-critical, SSE will populate
    }

    lastUpdated.value = new Date()
  }

  function startRefresh() {
    stopRefresh()

    // Fetch initial data
    fetchSummaries()
    fetchInitialEvents()

    // Subscribe to SSE for live events
    syslogUnsub = syslogStream.subscribe(onSyslogEvent)
    applogUnsub = applogStream.subscribe(onAppLogEvent)

    // Periodically refresh summaries (aggregated stats)
    summaryTimer = setInterval(fetchSummaries, SUMMARY_REFRESH_INTERVAL)
  }

  function setRange(r: string) {
    range_.value = r
    fetchSummaries()
    fetchInitialEvents()
  }

  function stopRefresh() {
    if (summaryTimer) {
      clearInterval(summaryTimer)
      summaryTimer = null
    }
    if (syslogUnsub) {
      syslogUnsub()
      syslogUnsub = null
    }
    if (applogUnsub) {
      applogUnsub()
      applogUnsub = null
    }
  }

  return {
    syslogSummary,
    applogSummary,
    recentSyslogEvents,
    recentApplogEvents,
    loading,
    loaded,
    error,
    lastUpdated,
    range: range_,
    syslogConnected: syslogStream.connected,
    applogConnected: applogStream.connected,
    startRefresh,
    stopRefresh,
    setRange,
  }
})
