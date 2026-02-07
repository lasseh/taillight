import { ref, computed } from 'vue'
import { defineStore } from 'pinia'
import { api } from '@/lib/api'
import type {
  RsyslogStatsSummary,
  RsyslogStatsTimeSeries,
  RsyslogStatsDataRecord,
} from '@/types/rsyslog-stats'

const REFRESH_INTERVAL = 60_000 // 60 seconds

export const useRsyslogStatsStore = defineStore('rsyslog-stats', () => {
  const summary = ref<RsyslogStatsSummary | null>(null)
  const ingestSeries = ref<RsyslogStatsTimeSeries[]>([])
  const queueSeries = ref<RsyslogStatsTimeSeries[]>([])
  const processedSeries = ref<RsyslogStatsTimeSeries[]>([])
  const failedSeries = ref<RsyslogStatsTimeSeries[]>([])

  const loading = ref(false)
  const error = ref<string | null>(null)

  const range_ = ref('1h')
  const interval_ = ref('1m')

  let refreshTimer: ReturnType<typeof setInterval> | null = null

  /** Convert raw time-series into flat records grouped by name for Unovis. */
  function toChartData(series: RsyslogStatsTimeSeries[]): RsyslogStatsDataRecord[] {
    const bucketMap = new Map<string, RsyslogStatsDataRecord>()
    for (const point of series) {
      const key = point.time
      let rec = bucketMap.get(key)
      if (!rec) {
        rec = { x: new Date(point.time).getTime() }
        bucketMap.set(key, rec)
      }
      rec[point.name] = (rec[point.name] ?? 0) + point.value
    }
    return [...bucketMap.values()].sort((a, b) => a.x - b.x)
  }

  /** Extract unique series names from time-series data. */
  function seriesNames(series: RsyslogStatsTimeSeries[]): string[] {
    const set = new Set<string>()
    for (const point of series) {
      set.add(point.name)
    }
    return [...set].sort()
  }

  const ingestChartData = computed(() => toChartData(ingestSeries.value))
  const queueChartData = computed(() => toChartData(queueSeries.value))
  const processedChartData = computed(() => toChartData(processedSeries.value))
  const failedChartData = computed(() => toChartData(failedSeries.value))

  const ingestNames = computed(() => seriesNames(ingestSeries.value))
  const queueNames = computed(() => seriesNames(queueSeries.value))
  const processedNames = computed(() => seriesNames(processedSeries.value))
  const failedNames = computed(() => seriesNames(failedSeries.value))

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

      const [summaryRes, ingestRes, queueRes, processedRes, failedRes] = await Promise.all([
        api.getRsyslogStatsSummary(range_.value),
        api.getRsyslogStatsVolume(new URLSearchParams([...params, ['field', 'submitted']])),
        api.getRsyslogStatsVolume(new URLSearchParams([...params, ['field', 'size']])),
        api.getRsyslogStatsVolume(new URLSearchParams([...params, ['field', 'processed']])),
        api.getRsyslogStatsVolume(new URLSearchParams([...params, ['field', 'failed']])),
      ])

      summary.value = summaryRes.data
      ingestSeries.value = ingestRes.data
      queueSeries.value = queueRes.data
      processedSeries.value = processedRes.data
      failedSeries.value = failedRes.data
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
    ingestSeries,
    queueSeries,
    processedSeries,
    failedSeries,
    ingestChartData,
    queueChartData,
    processedChartData,
    failedChartData,
    ingestNames,
    queueNames,
    processedNames,
    failedNames,
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
