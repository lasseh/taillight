import { ref, computed } from 'vue'
import { defineStore } from 'pinia'
import { api } from '@/lib/api'
import type { HostEntry } from '@/types/host'

const REFRESH_INTERVAL = 30_000 // 30 seconds

export const useHostsStore = defineStore('hosts', () => {
  const hosts = ref<HostEntry[]>([])
  const range_ = ref(localStorage.getItem('hosts-range') ?? '24h')
  const loading = ref(false)
  const error = ref<string | null>(null)
  const expanded = ref<Set<string>>(new Set())

  // Filters + sort (client-side).
  const search = ref('')
  const statusFilter = ref<'all' | 'healthy' | 'warning' | 'critical'>('all')
  const feedFilter = ref<'all' | 'srvlog' | 'netlog' | 'both'>('all')
  const sortBy = ref<'errors' | 'total' | 'hostname' | 'last_seen' | 'trend'>('errors')
  const groupBy = ref<'none' | 'feed' | 'status'>('none')

  let refreshTimer: ReturnType<typeof setInterval> | null = null

  const filteredHosts = computed(() => {
    let result = hosts.value

    // Search filter.
    if (search.value) {
      const q = search.value.toLowerCase()
      result = result.filter((h) => h.hostname.toLowerCase().includes(q))
    }

    // Status filter.
    if (statusFilter.value !== 'all') {
      result = result.filter((h) => h.status === statusFilter.value)
    }

    // Feed filter.
    if (feedFilter.value !== 'all') {
      result = result.filter((h) => h.feed === feedFilter.value)
    }

    // Sort.
    const sorted = [...result]
    sorted.sort((a, b) => {
      switch (sortBy.value) {
        case 'errors':
          return b.error_count - a.error_count
        case 'total':
          return b.total_count - a.total_count
        case 'hostname':
          return a.hostname.localeCompare(b.hostname)
        case 'last_seen': {
          const ta = a.last_seen_at ? new Date(a.last_seen_at).getTime() : 0
          const tb = b.last_seen_at ? new Date(b.last_seen_at).getTime() : 0
          return tb - ta
        }
        case 'trend':
          return b.trend - a.trend
        default:
          return 0
      }
    })

    return sorted
  })

  const statusCounts = computed(() => {
    const counts = { total: 0, healthy: 0, warning: 0, critical: 0 }
    for (const h of hosts.value) {
      counts.total++
      counts[h.status]++
    }
    return counts
  })

  async function fetchHosts() {
    loading.value = true
    try {
      const res = await api.getHostsSummary(range_.value)
      hosts.value = res.data ?? []
      error.value = null
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'unknown error'
    } finally {
      loading.value = false
    }
  }

  function setRange(r: string) {
    range_.value = r
    localStorage.setItem('hosts-range', r)
    fetchHosts()
  }

  function setStatusFilter(s: 'all' | 'healthy' | 'warning' | 'critical') {
    statusFilter.value = s
  }

  function toggle(hostname: string) {
    if (expanded.value.has(hostname)) {
      expanded.value.delete(hostname)
    } else {
      expanded.value.add(hostname)
    }
  }

  function collapseAll() {
    expanded.value.clear()
  }

  function startRefresh() {
    fetchHosts()
    refreshTimer = setInterval(fetchHosts, REFRESH_INTERVAL)
  }

  function stopRefresh() {
    if (refreshTimer) {
      clearInterval(refreshTimer)
      refreshTimer = null
    }
  }

  return {
    hosts,
    range_,
    loading,
    error,
    expanded,
    search,
    statusFilter,
    feedFilter,
    sortBy,
    groupBy,
    filteredHosts,
    statusCounts,
    fetchHosts,
    setRange,
    setStatusFilter,
    toggle,
    collapseAll,
    startRefresh,
    stopRefresh,
  }
})
