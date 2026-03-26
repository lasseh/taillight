import type { NetlogEvent } from '@/types/netlog'
import { api } from '@/lib/api'
import { useNetlogStream } from '@/composables/useNetlogStream'
import { useNetlogFilterStore } from '@/stores/netlog-filters'
import { createEventStore } from '@/stores/event-store-factory'
import { wildcardMatch } from '@/lib/wildcard'

function matchesFilters(event: NetlogEvent, filters: Record<string, string>): boolean {
  if (filters.from || filters.to) return false
  if (filters.hostname) {
    if (filters.hostname.includes('*') ? !wildcardMatch(event.hostname, filters.hostname) : event.hostname !== filters.hostname) return false
  }
  if (filters.programname && event.programname !== filters.programname) return false
  if (filters.syslogtag && event.syslogtag !== filters.syslogtag) return false
  if (filters.facility && event.facility !== Number(filters.facility)) return false
  if (filters.severity && event.severity !== Number(filters.severity)) return false
  if (filters.severity_max && event.severity > Number(filters.severity_max)) return false
  if (filters.search && !event.message.toLowerCase().includes(filters.search.toLowerCase())) return false
  return true
}

export const useNetlogEventStore = createEventStore({
  id: 'netlog-events',
  routeName: 'netlog',
  fetchEvents: (params, signal) => api.getNetlogs(params, signal),
  useStream: useNetlogStream,
  useFilterStore: useNetlogFilterStore,
  matchesFilters,
})
