import { ref } from 'vue'
import { defineStore } from 'pinia'
import { api, ApiError } from '@/lib/api'
import { useSyslogStream } from '@/composables/useSyslogStream'
import { useAppLogStream } from '@/composables/useAppLogStream'
import type { SyslogSummary, AppLogSummary, VolumeBucket, SeverityVolumeBucket } from '@/types/stats'
import type { SyslogEvent } from '@/types/syslog'
import type { AppLogEvent } from '@/types/applog'

const SUMMARY_REFRESH_INTERVAL = 30_000 // 30 seconds
const MAX_RECENT_EVENTS = 10
const HIGH_SEVERITY_MAX = 2 // syslog: crit and above
const HIGH_APPLOG_LEVELS = new Set(['FATAL', 'ERROR', 'WARN'])

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

/** Convert VolumeBucket[] → Record<"YYYY-MM-DD HH:mm", number> for heatmap. */
function volumeToHeatmap(buckets: VolumeBucket[]): Record<string, number> {
  const map: Record<string, number> = {}
  for (const b of buckets) {
    const d = new Date(b.time)
    const key = `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())} ${pad2(d.getHours())}:${pad2(d.getMinutes())}`
    map[key] = b.total
  }
  return map
}

function pad2(n: number): string {
  return String(n).padStart(2, '0')
}

export const useHomeStore = defineStore('home', () => {
  const syslogSummary = ref<SyslogSummary | null>(null)
  const applogSummary = ref<AppLogSummary | null>(null)
  const recentSyslogEvents = ref<SyslogEvent[]>([])
  const recentApplogEvents = ref<AppLogEvent[]>([])
  const syslogHeatmap = ref<Record<string, number>>({})
  const applogHeatmap = ref<Record<string, number>>({})
  const syslogSeverityVolume = ref<SeverityVolumeBucket[]>([])
  const applogSeverityVolume = ref<SeverityVolumeBucket[]>([])
  const loading = ref(false)
  const loaded = ref(false)
  const error = ref<string | null>(null)
  const lastUpdated = ref<Date | null>(null)
  const range_ = ref(localStorage.getItem('home-range') ?? '24h')

  let refreshTimer: ReturnType<typeof setInterval> | null = null
  let fetchVersion = 0
  let unsubSyslog: (() => void) | null = null
  let unsubApplog: (() => void) | null = null
  const syslogSeenIds = new Set<number>()
  const applogSeenIds = new Set<number>()

  // ── SSE handlers: prepend matching live events ──

  function onSyslogEvent(event: SyslogEvent) {
    if (event.severity > HIGH_SEVERITY_MAX) return
    if (syslogSeenIds.has(event.id)) return
    syslogSeenIds.add(event.id)
    recentSyslogEvents.value = [event, ...recentSyslogEvents.value].slice(0, MAX_RECENT_EVENTS)
  }

  function onApplogEvent(event: AppLogEvent) {
    if (!HIGH_APPLOG_LEVELS.has(event.level)) return
    if (applogSeenIds.has(event.id)) return
    applogSeenIds.add(event.id)
    recentApplogEvents.value = [event, ...recentApplogEvents.value].slice(0, MAX_RECENT_EVENTS)
  }

  // ── Fetchers ──

  async function fetchSummaries() {
    if (!loaded.value) {
      loading.value = true
    }

    let syslogErr: unknown = null
    let applogErr: unknown = null

    // Fetch independently so one failure doesn't block the other.
    try {
      const res = await api.getSyslogSummary(range_.value)
      syslogSummary.value = res.data
    } catch (e) {
      syslogErr = e
    }

    try {
      const res = await api.getAppLogSummary(range_.value)
      applogSummary.value = res.data
    } catch (e) {
      applogErr = e
    }

    // If both failed with network errors, show a single clean message.
    if (syslogErr && applogErr) {
      const isNetwork = (e: unknown) => !(e instanceof ApiError)
      if (isNetwork(syslogErr) && isNetwork(applogErr)) {
        error.value = 'connection'
      } else {
        const msg = (e: unknown) => e instanceof Error ? e.message : 'unknown error'
        error.value = `syslog: ${msg(syslogErr)}; applog: ${msg(applogErr)}`
      }
    } else if (syslogErr) {
      const msg = syslogErr instanceof Error ? syslogErr.message : 'unknown error'
      error.value = `syslog summary: ${msg}`
    } else if (applogErr) {
      const msg = applogErr instanceof Error ? applogErr.message : 'unknown error'
      error.value = `applog summary: ${msg}`
    } else {
      error.value = null
    }

    loading.value = false
    loaded.value = true
  }

  async function fetchRecentEvents() {
    const version = ++fetchVersion
    const from = rangeToFrom(range_.value)

    try {
      const syslogEventsRes = await api.getSyslogs(
        new URLSearchParams({ severity_max: String(HIGH_SEVERITY_MAX), limit: String(MAX_RECENT_EVENTS), from }),
      )
      if (version !== fetchVersion) return
      const events = (syslogEventsRes.data ?? []).slice(-MAX_RECENT_EVENTS)
      recentSyslogEvents.value = events
      syslogSeenIds.clear()
      for (const e of events) syslogSeenIds.add(e.id)
    } catch {
      // Non-critical — keep existing data
    }

    try {
      const applogEventsRes = await api.getAppLogs(
        new URLSearchParams({ level: 'WARN', limit: String(MAX_RECENT_EVENTS), from }),
      )
      if (version !== fetchVersion) return
      const events = (applogEventsRes.data ?? []).slice(-MAX_RECENT_EVENTS)
      recentApplogEvents.value = events
      applogSeenIds.clear()
      for (const e of events) applogSeenIds.add(e.id)
    } catch {
      // Non-critical — keep existing data
    }

    if (version === fetchVersion) {
      lastUpdated.value = new Date()
    }
  }

  async function fetchHeatmaps() {
    const params = new URLSearchParams({ interval: '30m', range: '7d' })

    try {
      const res = await api.getVolume(params)
      syslogHeatmap.value = volumeToHeatmap(res.data ?? [])
    } catch {
      // Non-critical — keep existing data
    }

    try {
      const res = await api.getAppLogVolume(params)
      applogHeatmap.value = volumeToHeatmap(res.data ?? [])
    } catch {
      // Non-critical — keep existing data
    }
  }

  async function fetchSeverityTimelines() {
    const params = new URLSearchParams({ interval: '15m', range: '24h' })

    try {
      const res = await api.getSeverityVolume(params)
      syslogSeverityVolume.value = res.data ?? []
    } catch {
      // Non-critical — keep existing data
    }

    try {
      const res = await api.getAppLogSeverityVolume(params)
      applogSeverityVolume.value = res.data ?? []
    } catch {
      // Non-critical — keep existing data
    }
  }

  /** Summaries and heatmaps poll; recent events stay live via SSE. */
  function refreshPolled() {
    fetchSummaries()
    fetchHeatmaps()
    fetchSeverityTimelines()
  }

  function subscribeStreams() {
    const syslog = useSyslogStream()
    const applog = useAppLogStream()
    unsubSyslog = syslog.subscribe(onSyslogEvent)
    unsubApplog = applog.subscribe(onApplogEvent)
  }

  function unsubscribeStreams() {
    unsubSyslog?.()
    unsubApplog?.()
    unsubSyslog = null
    unsubApplog = null
  }

  function startRefresh() {
    stopRefresh()
    fetchSummaries()
    fetchRecentEvents()
    fetchHeatmaps()
    fetchSeverityTimelines()
    subscribeStreams()
    refreshTimer = setInterval(refreshPolled, SUMMARY_REFRESH_INTERVAL)
  }

  function setRange(r: string) {
    range_.value = r
    localStorage.setItem('home-range', r)
    recentSyslogEvents.value = []
    recentApplogEvents.value = []
    syslogSeenIds.clear()
    applogSeenIds.clear()
    fetchSummaries()
    fetchRecentEvents()
    fetchHeatmaps()
    fetchSeverityTimelines()
  }

  function stopRefresh() {
    if (refreshTimer) {
      clearInterval(refreshTimer)
      refreshTimer = null
    }
    unsubscribeStreams()
  }

  return {
    syslogSummary,
    applogSummary,
    recentSyslogEvents,
    recentApplogEvents,
    syslogHeatmap,
    applogHeatmap,
    syslogSeverityVolume,
    applogSeverityVolume,
    loading,
    loaded,
    error,
    lastUpdated,
    range: range_,
    startRefresh,
    stopRefresh,
    setRange,
  }
})
