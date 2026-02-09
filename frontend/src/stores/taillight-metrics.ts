import { ref, computed } from 'vue'
import { defineStore } from 'pinia'
import { api } from '@/lib/api'
import type {
  TaillightMetricsSummary,
  TaillightMetricsTimeSeries,
} from '@/types/taillight-metrics'
import type { SimplePoint } from '@/types/chart'

const REFRESH_INTERVAL = 60_000 // 60 seconds

export const useTaillightMetricsStore = defineStore('taillight-metrics', () => {
  const summary = ref<TaillightMetricsSummary | null>(null)
  const eventsBroadcastSeries = ref<TaillightMetricsTimeSeries[]>([])
  const applogBroadcastSeries = ref<TaillightMetricsTimeSeries[]>([])
  const sseClientsSyslogSeries = ref<TaillightMetricsTimeSeries[]>([])
  const sseClientsApplogSeries = ref<TaillightMetricsTimeSeries[]>([])
  const dbPoolActiveSeries = ref<TaillightMetricsTimeSeries[]>([])
  const dbPoolIdleSeries = ref<TaillightMetricsTimeSeries[]>([])
  const dbPoolTotalSeries = ref<TaillightMetricsTimeSeries[]>([])

  const loading = ref(false)
  const error = ref<string | null>(null)

  const range_ = ref('1h')
  const interval_ = ref('1m')

  let refreshTimer: ReturnType<typeof setInterval> | null = null

  function toSimpleLine(series: TaillightMetricsTimeSeries[]): SimplePoint[] {
    return series
      .map((p) => ({ x: new Date(p.time).getTime(), y: p.value }))
      .sort((a, b) => a.x - b.x)
  }

  const eventsBroadcastLine = computed(() => toSimpleLine(eventsBroadcastSeries.value))
  const applogBroadcastLine = computed(() => toSimpleLine(applogBroadcastSeries.value))
  const sseClientsSyslogLine = computed(() => toSimpleLine(sseClientsSyslogSeries.value))
  const sseClientsApplogLine = computed(() => toSimpleLine(sseClientsApplogSeries.value))
  const dbPoolActiveLine = computed(() => toSimpleLine(dbPoolActiveSeries.value))
  const dbPoolIdleLine = computed(() => toSimpleLine(dbPoolIdleSeries.value))
  const dbPoolTotalLine = computed(() => toSimpleLine(dbPoolTotalSeries.value))

  async function fetchAll() {
    if (!summary.value) {
      loading.value = true
    }
    error.value = null

    try {
      const params = new URLSearchParams({
        interval: interval_.value,
        range: range_.value,
      })

      const [
        summaryRes,
        eventsBroadcastRes,
        applogBroadcastRes,
        sseClientsSyslogRes,
        sseClientsApplogRes,
        dbPoolActiveRes,
        dbPoolIdleRes,
        dbPoolTotalRes,
      ] = await Promise.all([
        api.getTaillightMetricsSummary(range_.value),
        api.getTaillightMetricsVolume(new URLSearchParams([...params, ['field', 'events_broadcast']])),
        api.getTaillightMetricsVolume(new URLSearchParams([...params, ['field', 'applog_events_broadcast']])),
        api.getTaillightMetricsVolume(new URLSearchParams([...params, ['field', 'sse_clients_syslog']])),
        api.getTaillightMetricsVolume(new URLSearchParams([...params, ['field', 'sse_clients_applog']])),
        api.getTaillightMetricsVolume(new URLSearchParams([...params, ['field', 'db_pool_active']])),
        api.getTaillightMetricsVolume(new URLSearchParams([...params, ['field', 'db_pool_idle']])),
        api.getTaillightMetricsVolume(new URLSearchParams([...params, ['field', 'db_pool_total']])),
      ])

      summary.value = summaryRes.data
      eventsBroadcastSeries.value = eventsBroadcastRes.data
      applogBroadcastSeries.value = applogBroadcastRes.data
      sseClientsSyslogSeries.value = sseClientsSyslogRes.data
      sseClientsApplogSeries.value = sseClientsApplogRes.data
      dbPoolActiveSeries.value = dbPoolActiveRes.data
      dbPoolIdleSeries.value = dbPoolIdleRes.data
      dbPoolTotalSeries.value = dbPoolTotalRes.data
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'failed to load taillight metrics'
    } finally {
      loading.value = false
    }
  }

  function setPreset(r: string, i: string) {
    range_.value = r
    interval_.value = i
    fetchAll()
  }

  function startRefresh() {
    stopRefresh()
    fetchAll()
    refreshTimer = setInterval(fetchAll, REFRESH_INTERVAL)
  }

  function stopRefresh() {
    if (refreshTimer) {
      clearInterval(refreshTimer)
      refreshTimer = null
    }
  }

  return {
    summary,
    eventsBroadcastLine,
    applogBroadcastLine,
    sseClientsSyslogLine,
    sseClientsApplogLine,
    dbPoolActiveLine,
    dbPoolIdleLine,
    dbPoolTotalLine,
    loading,
    error,
    range: range_,
    interval: interval_,
    fetchAll,
    setPreset,
    startRefresh,
    stopRefresh,
  }
})
