import { ref, watch } from 'vue'
import { defineStore } from 'pinia'
import { api } from '@/lib/api'
import { useDebounceFn } from '@/composables/useDebounceFn'
import type { HostEntry, HostSort } from '@/types/host'

const REFRESH_INTERVAL = 30_000 // 30 seconds
const PAGE_SIZE = 100
const SEARCH_DEBOUNCE_MS = 300

export const useHostsStore = defineStore('hosts', () => {
  // One page of hosts. The server applies the search, filters, sort and
  // paging; total counts every host that matches.
  const hosts = ref<HostEntry[]>([])
  const total = ref(0)
  const offset = ref(0)
  const pageSize = PAGE_SIZE
  const range_ = ref(localStorage.getItem('hosts-range') ?? '24h')
  const loading = ref(false)
  const error = ref<string | null>(null)
  const expanded = ref<Set<string>>(new Set())

  // Filters + sort (sent to the server).
  const search = ref('')
  const statusFilter = ref<'all' | 'healthy' | 'warning' | 'critical'>('all')
  const feedFilter = ref<'all' | 'srvlog' | 'netlog' | 'both'>('all')
  const sortBy = ref<HostSort>('errors')
  const groupBy = ref<'none' | 'feed' | 'status'>('none')

  let refreshTimer: ReturnType<typeof setInterval> | null = null
  let fetchVersion = 0

  async function fetchHosts() {
    const version = ++fetchVersion
    loading.value = true
    const params = new URLSearchParams({
      range: range_.value,
      sort: sortBy.value,
      limit: String(PAGE_SIZE),
      offset: String(offset.value),
    })
    if (search.value) params.set('search', search.value)
    if (statusFilter.value !== 'all') params.set('status', statusFilter.value)
    if (feedFilter.value !== 'all') params.set('feed', feedFilter.value)
    try {
      const res = await api.getHostsSummary(params)
      if (version !== fetchVersion) return
      // Hosts aged out between polls can leave the offset past the end.
      if (!res.data?.length && offset.value > 0) {
        offset.value = 0
        fetchHosts()
        return
      }
      hosts.value = res.data ?? []
      total.value = res.total ?? 0
      error.value = null
    } catch (e) {
      if (version !== fetchVersion) return
      error.value = e instanceof Error ? e.message : 'unknown error'
    } finally {
      if (version === fetchVersion) loading.value = false
    }
  }

  // A new search, filter, sort or range starts again at the first page.
  function fetchFirstPage() {
    offset.value = 0
    fetchHosts()
  }
  watch(search, useDebounceFn(fetchFirstPage, SEARCH_DEBOUNCE_MS))
  watch([statusFilter, feedFilter, sortBy], fetchFirstPage)

  function nextPage() {
    if (offset.value + PAGE_SIZE >= total.value) return
    offset.value += PAGE_SIZE
    fetchHosts()
  }

  function prevPage() {
    if (offset.value === 0) return
    offset.value = Math.max(0, offset.value - PAGE_SIZE)
    fetchHosts()
  }

  function setRange(r: string) {
    range_.value = r
    localStorage.setItem('hosts-range', r)
    fetchFirstPage()
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
    total,
    offset,
    pageSize,
    range_,
    loading,
    error,
    expanded,
    search,
    statusFilter,
    feedFilter,
    sortBy,
    groupBy,
    fetchHosts,
    nextPage,
    prevPage,
    setRange,
    setStatusFilter,
    toggle,
    collapseAll,
    startRefresh,
    stopRefresh,
  }
})
