import { ref, computed } from 'vue'
import { defineStore } from 'pinia'
import { api } from '@/lib/api'
import type {
  RsyslogStatsSummary,
  RsyslogStatsTimeSeries,
} from '@/types/rsyslog-stats'
import type { SimplePoint } from '@/types/chart'

const REFRESH_INTERVAL = 60_000 // 60 seconds

export const useRsyslogStatsStore = defineStore('rsyslog-stats', () => {
  const summary = ref<RsyslogStatsSummary | null>(null)
  const submittedSeries = ref<RsyslogStatsTimeSeries[]>([])
  const processedSeries = ref<RsyslogStatsTimeSeries[]>([])
  const queueSeries = ref<RsyslogStatsTimeSeries[]>([])

  const loading = ref(false)
  const error = ref<string | null>(null)

  const range_ = ref('1h')
  const interval_ = ref('1m')

  let refreshTimer: ReturnType<typeof setInterval> | null = null

  /** Aggregate time-series across all components into simple totals per bucket. */
  function toSimpleLine(series: RsyslogStatsTimeSeries[]): SimplePoint[] {
    const bucketMap = new Map<string, number>()
    for (const point of series) {
      bucketMap.set(point.time, (bucketMap.get(point.time) ?? 0) + point.value)
    }
    return [...bucketMap.entries()]
      .map(([time, val]) => ({ x: new Date(time).getTime(), y: val }))
      .sort((a, b) => a.x - b.x)
  }

  const submittedLine = computed(() => toSimpleLine(submittedSeries.value))
  const processedLine = computed(() => toSimpleLine(processedSeries.value))
  const queueLine = computed(() => toSimpleLine(queueSeries.value))

  async function fetchAll() {
    loading.value = true
    error.value = null

    try {
      const params = new URLSearchParams({
        interval: interval_.value,
        range: range_.value,
      })

      const [summaryRes, submittedRes, processedRes, queueRes] = await Promise.all([
        api.getRsyslogStatsSummary(range_.value),
        api.getRsyslogStatsVolume(new URLSearchParams([...params, ['field', 'submitted']])),
        api.getRsyslogStatsVolume(new URLSearchParams([...params, ['field', 'processed']])),
        api.getRsyslogStatsVolume(new URLSearchParams([...params, ['field', 'size']])),
      ])

      summary.value = summaryRes.data
      submittedSeries.value = submittedRes.data
      processedSeries.value = processedRes.data
      queueSeries.value = queueRes.data
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'failed to load rsyslog stats'
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
    submittedLine,
    processedLine,
    queueLine,
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
