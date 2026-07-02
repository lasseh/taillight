import { ref, computed } from 'vue'
import { defineStore } from 'pinia'
import { api, ApiError } from '@/lib/api'
import { useSrvlogStream } from '@/composables/useSrvlogStream'
import { useNetlogStream } from '@/composables/useNetlogStream'
import { useAppLogStream } from '@/composables/useAppLogStream'
import type {
  SrvlogSummary,
  AppLogSummary,
  VolumeBucket,
  SeverityVolumeBucket,
} from '@/types/stats'
import type { SrvlogEvent } from '@/types/srvlog'
import type { NetlogEvent } from '@/types/netlog'
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
  const netlogSummary = ref<SrvlogSummary | null>(null)
  const applogSummary = ref<AppLogSummary | null>(null)
  const recentSrvlogEvents = ref<SrvlogEvent[]>([])
  const recentNetlogEvents = ref<NetlogEvent[]>([])
  const recentApplogEvents = ref<AppLogEvent[]>([])

  // Cross-feed shaping: merge the srvlog + netlog recent feeds into one
  // newest-first list tagged with a feed badge + detail route, capped at
  // MAX_RECENT_EVENTS. Lives in the store (not the view) so the shaping is
  // testable and reusable.
  const combinedRecentEvents = computed(() => {
    const s = recentSrvlogEvents.value.map((e) => ({
      ...e,
      _feed: 'srvlog' as const,
      _routeName: 'srvlog-detail',
    }))
    const n = recentNetlogEvents.value.map((e) => ({
      ...e,
      _feed: 'netlog' as const,
      _routeName: 'netlog-detail',
    }))
    return [...s, ...n]
      .sort((a, b) => new Date(b.received_at).getTime() - new Date(a.received_at).getTime())
      .slice(0, MAX_RECENT_EVENTS)
  })
  const srvlogHeatmap = ref<Record<string, number>>({})
  const netlogHeatmap = ref<Record<string, number>>({})
  const applogHeatmap = ref<Record<string, number>>({})
  const srvlogSeverityVolume = ref<SeverityVolumeBucket[]>([])
  const netlogSeverityVolume = ref<SeverityVolumeBucket[]>([])
  const applogSeverityVolume = ref<SeverityVolumeBucket[]>([])

  // ── Cross-feed shaping: combined srvlog+netlog "syslog" getters ──
  // Same rationale as combinedRecentEvents: derivation lives in the store so
  // it is testable and reusable, not re-authored in views.

  // Syslog: combined total & trend
  const syslogTotal = computed(
    () => (srvlogSummary.value?.total ?? 0) + (netlogSummary.value?.total ?? 0),
  )

  const syslogTrend = computed(() => {
    const s = srvlogSummary.value
    const n = netlogSummary.value
    if (!s && !n) return 0
    const curr = syslogTotal.value
    const sPrev = s && s.trend !== 0 ? s.total / (1 + s.trend / 100) : (s?.total ?? 0)
    const nPrev = n && n.trend !== 0 ? n.total / (1 + n.trend / 100) : (n?.total ?? 0)
    const prev = sPrev + nPrev
    if (prev === 0) return curr > 0 ? 100 : 0
    return ((curr - prev) / prev) * 100
  })

  // Syslog: combined severity breakdown for SeverityDistribution component
  const syslogSeverityBreakdown = computed(() => {
    const srvlog = srvlogSummary.value?.severity_breakdown ?? []
    const netlog = netlogSummary.value?.severity_breakdown ?? []
    const total = syslogTotal.value
    const map = new Map<number, { severity: number; label: string; count: number }>()
    for (const s of [...srvlog, ...netlog]) {
      const existing = map.get(s.severity)
      if (existing) {
        existing.count += s.count
      } else {
        map.set(s.severity, { severity: s.severity, label: s.label, count: s.count })
      }
    }
    return [...map.values()]
      .map((s) => ({ ...s, pct: total > 0 ? (s.count / total) * 100 : 0 }))
      .sort((a, b) => a.severity - b.severity)
  })

  // Syslog: merged top hosts from both feeds
  const syslogTopHosts = computed(() => {
    const srvlog = srvlogSummary.value?.top_hosts ?? []
    const netlog = netlogSummary.value?.top_hosts ?? []
    const total = syslogTotal.value
    const srvlogNames = new Set(srvlog.map((h) => h.name))
    const map = new Map<string, number>()
    for (const h of [...srvlog, ...netlog]) {
      map.set(h.name, (map.get(h.name) ?? 0) + h.count)
    }
    return [...map.entries()]
      .map(([name, count]) => ({
        name,
        count,
        pct: total > 0 ? (count / total) * 100 : 0,
        feed: srvlogNames.has(name) ? 'srvlog' : ('netlog' as 'srvlog' | 'netlog'),
      }))
      .sort((a, b) => b.count - a.count)
  })

  // Syslog: combined heatmap
  const syslogHeatmap = computed(() => {
    const combined: Record<string, number> = {}
    for (const [key, val] of Object.entries(srvlogHeatmap.value)) {
      combined[key] = (combined[key] ?? 0) + val
    }
    for (const [key, val] of Object.entries(netlogHeatmap.value)) {
      combined[key] = (combined[key] ?? 0) + val
    }
    return combined
  })
  const loading = ref(false)
  const loaded = ref(false)
  const error = ref<string | null>(null)
  const lastUpdated = ref<Date | null>(null)
  const range_ = ref(localStorage.getItem('home-range') ?? '24h')

  let refreshTimer: ReturnType<typeof setInterval> | null = null
  let fetchVersion = 0
  let unsubSrvlog: (() => void) | null = null
  let unsubNetlog: (() => void) | null = null
  let unsubApplog: (() => void) | null = null
  const srvlogSeenIds = new Set<number>()
  const netlogSeenIds = new Set<number>()
  const applogSeenIds = new Set<number>()

  // Cap each dedup Set so a long-lived dashboard tab doesn't accumulate one id
  // per high-severity event forever. The displayed window is tiny
  // (MAX_RECENT_EVENTS); a few hundred remembered ids is far more than enough to
  // dedup SSE re-delivery (incl. the ~100-event reconnect backfill) while staying
  // bounded. Batch-evicts the oldest insertions, mirroring event-store-factory.
  const SEEN_IDS_HIGH = 600
  const SEEN_IDS_TRIM = 200
  function rememberSeen(seen: Set<number>, id: number) {
    seen.add(id)
    if (seen.size > SEEN_IDS_HIGH) {
      const iter = seen.values()
      for (let i = 0; i < SEEN_IDS_TRIM; i++) seen.delete(iter.next().value!)
    }
  }

  // ── SSE handlers: prepend matching live events ──

  function onSrvlogEvent(event: SrvlogEvent) {
    if (event.severity > HIGH_SEVERITY_MAX) return
    if (srvlogSeenIds.has(event.id)) return
    rememberSeen(srvlogSeenIds, event.id)
    recentSrvlogEvents.value = [event, ...recentSrvlogEvents.value].slice(0, MAX_RECENT_EVENTS)
  }

  function onNetlogEvent(event: NetlogEvent) {
    if (event.severity > HIGH_SEVERITY_MAX) return
    if (netlogSeenIds.has(event.id)) return
    rememberSeen(netlogSeenIds, event.id)
    recentNetlogEvents.value = [event, ...recentNetlogEvents.value].slice(0, MAX_RECENT_EVENTS)
  }

  function onApplogEvent(event: AppLogEvent) {
    if (!HIGH_APPLOG_LEVELS.has(event.level)) return
    if (applogSeenIds.has(event.id)) return
    rememberSeen(applogSeenIds, event.id)
    recentApplogEvents.value = [event, ...recentApplogEvents.value].slice(0, MAX_RECENT_EVENTS)
  }

  // ── Fetchers ──

  async function fetchSummaries() {
    if (!loaded.value) {
      loading.value = true
    }

    let srvlogErr: unknown = null
    let netlogErr: unknown = null
    let applogErr: unknown = null

    // Fetch independently so one failure doesn't block the other.
    try {
      const res = await api.getSrvlogSummary(range_.value)
      srvlogSummary.value = res.data
    } catch (e) {
      srvlogErr = e
    }

    try {
      const res = await api.getNetlogSummary(range_.value)
      netlogSummary.value = res.data
    } catch (e) {
      netlogErr = e
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
    const errMsg = (e: unknown) => (e instanceof Error && e.message ? e.message : 'unknown error')

    // Collect all errors from the feeds.
    const feedErrors: { name: string; err: unknown }[] = []
    if (srvlogErr) feedErrors.push({ name: 'srvlog', err: srvlogErr })
    if (netlogErr) feedErrors.push({ name: 'netlog', err: netlogErr })
    if (applogErr) feedErrors.push({ name: 'applog', err: applogErr })

    const allFailed = feedErrors.length === 3
    const allConnection = feedErrors.every((f) => isConnectionErr(f.err))

    if (allFailed && allConnection) {
      error.value = 'connection'
      startReconnect()
    } else if (feedErrors.length > 0) {
      error.value = feedErrors.map((f) => `${f.name}: ${errMsg(f.err)}`).join('; ')
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
        new URLSearchParams({
          severity_max: String(HIGH_SEVERITY_MAX),
          limit: String(MAX_RECENT_EVENTS),
          from,
        }),
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
      const netlogEventsRes = await api.getNetlogs(
        new URLSearchParams({
          severity_max: String(HIGH_SEVERITY_MAX),
          limit: String(MAX_RECENT_EVENTS),
          from,
        }),
      )
      if (version !== fetchVersion) return
      const events = (netlogEventsRes.data ?? []).slice(-MAX_RECENT_EVENTS)
      recentNetlogEvents.value = events
      netlogSeenIds.clear()
      for (const e of events) netlogSeenIds.add(e.id)
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
      const res = await api.getNetlogVolume(params)
      netlogHeatmap.value = volumeToHeatmap(res.data ?? [])
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
      const res = await api.getNetlogSeverityVolume(params)
      netlogSeverityVolume.value = res.data ?? []
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
    unsubSrvlog = srvlog.subscribe(onSrvlogEvent)
    const netlog = useNetlogStream()
    unsubNetlog = netlog.subscribe(onNetlogEvent)
    const applog = useAppLogStream()
    unsubApplog = applog.subscribe(onApplogEvent)
  }

  function unsubscribeStreams() {
    unsubSrvlog?.()
    unsubNetlog?.()
    unsubApplog?.()
    unsubSrvlog = null
    unsubNetlog = null
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
    recentNetlogEvents.value = []
    recentApplogEvents.value = []
    srvlogSeenIds.clear()
    netlogSeenIds.clear()
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

  /** Drop buffered event rows so the next session starts clean. Called on logout. */
  function reset() {
    // Invalidate any in-flight fetchRecentEvents so it can't repopulate.
    fetchVersion++
    recentSrvlogEvents.value = []
    recentNetlogEvents.value = []
    recentApplogEvents.value = []
    srvlogSeenIds.clear()
    netlogSeenIds.clear()
    applogSeenIds.clear()
  }

  return {
    srvlogSummary,
    netlogSummary,
    applogSummary,
    recentSrvlogEvents,
    recentNetlogEvents,
    recentApplogEvents,
    combinedRecentEvents,
    syslogTotal,
    syslogTrend,
    syslogSeverityBreakdown,
    syslogTopHosts,
    syslogHeatmap,
    srvlogHeatmap,
    netlogHeatmap,
    applogHeatmap,
    srvlogSeverityVolume,
    netlogSeverityVolume,
    applogSeverityVolume,
    loading,
    loaded,
    error,
    lastUpdated,
    range: range_,
    startRefresh,
    stopRefresh,
    setRange,
    reset,
  }
})
