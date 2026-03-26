import { ref } from 'vue'
import { defineStore } from 'pinia'
import { api, ApiError } from '@/lib/api'
import { useSrvlogStream } from '@/composables/useSrvlogStream'
import { useAppLogStream } from '@/composables/useAppLogStream'
import type { SrvlogSummary, AppLogSummary, VolumeBucket, SeverityVolumeBucket } from '@/types/stats'
import type { SrvlogEvent } from '@/types/srvlog'
import type { AppLogEvent } from '@/types/applog'

const SUMMARY_REFRESH_INTERVAL = 30_000 // 30 seconds
const RECONNECT_INTERVAL = 5_000 // 5 seconds when disconnected
const MAX_RECENT_EVENTS = 10
const HIGH_SEVERITY_MAX = 2 // srvlog: crit and above
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
  const srvlogSummary = ref<SrvlogSummary | null>(null)
  const applogSummary = ref<AppLogSummary | null>(null)
  const recentSrvlogEvents = ref<SrvlogEvent[]>([])
  const recentApplogEvents = ref<AppLogEvent[]>([])
  const srvlogHeatmap = ref<Record<string, number>>({})
  const applogHeatmap = ref<Record<string, number>>({})
  const srvlogSeverityVolume = ref<SeverityVolumeBucket[]>([])
  const applogSeverityVolume = ref<SeverityVolumeBucket[]>([])
  const loading = ref(false)
  const loaded = ref(false)
  const error = ref<string | null>(null)
  const lastUpdated = ref<Date | null>(null)
  const range_ = ref(localStorage.getItem('home-range') ?? '24h')

  let refreshTimer: ReturnType<typeof setInterval> | null = null
  let fetchVersion = 0
  let unsubSrvlog: (() => void) | null = null
  let unsubApplog: (() => void) | null = null
  const srvlogSeenIds = new Set<number>()
  const applogSeenIds = new Set<number>()

  // ── SSE handlers: prepend matching live events ──

  function onSrvlogEvent(event: SrvlogEvent) {
    if (event.severity > HIGH_SEVERITY_MAX) return
    if (srvlogSeenIds.has(event.id)) return
    srvlogSeenIds.add(event.id)
    recentSrvlogEvents.value = [event, ...recentSrvlogEvents.value].slice(0, MAX_RECENT_EVENTS)
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

    let srvlogErr: unknown = null
    let applogErr: unknown = null

    // Fetch independently so one failure doesn't block the other.
    try {
      const res = await api.getSrvlogSummary(range_.value)
      srvlogSummary.value = res.data
    } catch (e) {
      srvlogErr = e
    }

    try {
      const res = await api.getAppLogSummary(range_.value)
      applogSummary.value = res.data
    } catch (e) {
      applogErr = e
    }

    // Detect connection-level failures: network errors or gateway errors (502-504).
    const isConnectionErr = (e: unknown) =>
      !(e instanceof ApiError) || (e.status >= 502 && e.status <= 504)
    const errMsg = (e: unknown) =>
      (e instanceof Error && e.message) ? e.message : 'unknown error'

    if (srvlogErr && applogErr && isConnectionErr(srvlogErr) && isConnectionErr(applogErr)) {
      error.value = 'connection'
      startReconnect()
    } else if (srvlogErr && applogErr) {
      error.value = `srvlog: ${errMsg(srvlogErr)}; applog: ${errMsg(applogErr)}`
    } else if (srvlogErr) {
      error.value = `srvlog summary: ${errMsg(srvlogErr)}`
    } else if (applogErr) {
      error.value = `applog summary: ${errMsg(applogErr)}`
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
      const srvlogEventsRes = await api.getSrvlogs(
        new URLSearchParams({ severity_max: String(HIGH_SEVERITY_MAX), limit: String(MAX_RECENT_EVENTS), from }),
      )
      if (version !== fetchVersion) return
      const events = (srvlogEventsRes.data ?? []).slice(-MAX_RECENT_EVENTS)
      recentSrvlogEvents.value = events
      srvlogSeenIds.clear()
      for (const e of events) srvlogSeenIds.add(e.id)
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
      const res = await api.getSrvlogVolume(params)
      srvlogHeatmap.value = volumeToHeatmap(res.data ?? [])
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
      const res = await api.getSrvlogSeverityVolume(params)
      srvlogSeverityVolume.value = res.data ?? []
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

  let reconnectTimer: ReturnType<typeof setInterval> | null = null

  /** Normal polling: refresh summaries, heatmaps, timelines. */
  function refreshPolled() {
    fetchSummaries()
    fetchHeatmaps()
    fetchSeverityTimelines()
  }

  function subscribeStreams() {
    const srvlog = useSrvlogStream()
    const applog = useAppLogStream()
    unsubSrvlog = srvlog.subscribe(onSrvlogEvent)
    unsubApplog = applog.subscribe(onApplogEvent)
  }

  function unsubscribeStreams() {
    unsubSrvlog?.()
    unsubApplog?.()
    unsubSrvlog = null
    unsubApplog = null
  }

  function stopReconnect() {
    if (reconnectTimer) {
      clearInterval(reconnectTimer)
      reconnectTimer = null
    }
  }

  /** Switch to fast reconnect polling (summaries only). */
  function startReconnect() {
    if (reconnectTimer) return // already reconnecting
    // Pause normal polling and SSE while disconnected.
    if (refreshTimer) {
      clearInterval(refreshTimer)
      refreshTimer = null
    }
    unsubscribeStreams()

    reconnectTimer = setInterval(async () => {
      await fetchSummaries()
      if (error.value === null) {
        // Server is back — stop reconnect, do a full data reload.
        stopReconnect()
        fetchRecentEvents()
        fetchHeatmaps()
        fetchSeverityTimelines()
        subscribeStreams()
        refreshTimer = setInterval(refreshPolled, SUMMARY_REFRESH_INTERVAL)
      }
    }, RECONNECT_INTERVAL)
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
    recentSrvlogEvents.value = []
    recentApplogEvents.value = []
    srvlogSeenIds.clear()
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
    stopReconnect()
    unsubscribeStreams()
  }

  return {
    srvlogSummary,
    applogSummary,
    recentSrvlogEvents,
    recentApplogEvents,
    srvlogHeatmap,
    applogHeatmap,
    srvlogSeverityVolume,
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
