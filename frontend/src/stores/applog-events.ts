import type { AppLogEvent } from '@/types/applog'
import { api } from '@/lib/api'
import { useAppLogStream } from '@/composables/useAppLogStream'
import { useAppLogFilterStore } from '@/stores/applog-filters'
import { createEventStore } from '@/stores/event-store-factory'
import { LEVEL_RANK } from '@/lib/applog-constants'

function matchesFilters(event: AppLogEvent, filters: Record<string, string>): boolean {
  if (filters.service && event.service !== filters.service) return false
  if (filters.component && event.component !== filters.component) return false
  if (filters.host && event.host !== filters.host) return false
  // Level filter: include events at or above the selected level (lower rank = more severe).
  if (filters.level) {
    const filterRank = LEVEL_RANK[filters.level] ?? 99
    const eventRank = LEVEL_RANK[event.level] ?? 99
    if (eventRank > filterRank) return false
  }
  if (filters.search && !event.msg.toLowerCase().includes(filters.search.toLowerCase())) return false
  return true
}

export const useAppLogEventStore = createEventStore({
  id: 'applog-events',
  routeName: 'applog',
  fetchEvents: (params) => api.getAppLogs(params),
  useStream: useAppLogStream,
  useFilterStore: useAppLogFilterStore,
  matchesFilters,
})
