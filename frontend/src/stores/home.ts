import { ref } from 'vue'
import { defineStore } from 'pinia'
import { api } from '@/lib/api'
import type { SyslogSummary, AppLogSummary, VolumeBucket } from '@/types/stats'
import type { SyslogEvent } from '@/types/syslog'
import type { AppLogEvent } from '@/types/applog'

const SUMMARY_REFRESH_INTERVAL = 30_000 // 30 seconds
const MAX_RECENT_EVENTS = 10
const HIGH_SEVERITY_MAX = 2 // syslog: crit and above

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
  const loading = ref(false)
  const loaded = ref(false)
  const error = ref<string | null>(null)
  const lastUpdated = ref<Date | null>(null)
  const range_ = ref(localStorage.getItem('home-range') ?? '24h')

  let refreshTimer: ReturnType<typeof setInterval> | null = null
  let fetchVersion = 0

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

  async function fetchRecentEvents() {
    const version = ++fetchVersion
    const from = rangeToFrom(range_.value)

    try {
      const syslogEventsRes = await api.getSyslogs(
        new URLSearchParams({ severity_max: String(HIGH_SEVERITY_MAX), limit: String(MAX_RECENT_EVENTS), from }),
      )
      if (version !== fetchVersion) return
      recentSyslogEvents.value = (syslogEventsRes.data ?? []).slice(-MAX_RECENT_EVENTS)
    } catch {
      // Non-critical — keep existing data
    }

    try {
      const applogEventsRes = await api.getAppLogs(
        new URLSearchParams({ level: 'WARN', limit: String(MAX_RECENT_EVENTS), from }),
      )
      if (version !== fetchVersion) return
      recentApplogEvents.value = (applogEventsRes.data ?? []).slice(-MAX_RECENT_EVENTS)
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

  function refresh() {
    fetchSummaries()
    fetchRecentEvents()
    fetchHeatmaps()
  }

  function startRefresh() {
    stopRefresh()
    refresh()
    refreshTimer = setInterval(refresh, SUMMARY_REFRESH_INTERVAL)
  }

  function setRange(r: string) {
    range_.value = r
    localStorage.setItem('home-range', r)
    recentSyslogEvents.value = []
    recentApplogEvents.value = []
    refresh()
  }

  function stopRefresh() {
    if (refreshTimer) {
      clearInterval(refreshTimer)
      refreshTimer = null
    }
  }

  return {
    syslogSummary,
    applogSummary,
    recentSyslogEvents,
    recentApplogEvents,
    syslogHeatmap,
    applogHeatmap,
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
