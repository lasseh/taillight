import type { SrvlogEvent } from '@/types/srvlog'
import { api } from '@/lib/api'
import { useSrvlogStream } from '@/composables/useSrvlogStream'
import { useSrvlogFilterStore } from '@/stores/srvlog-filters'
import { createEventStore } from '@/stores/event-store-factory'
import { wildcardMatch } from '@/lib/wildcard'

function matchesFilters(event: SrvlogEvent, filters: Record<string, string>): boolean {
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

export const useSrvlogEventStore = createEventStore({
  id: 'srvlog-events',
  routeName: 'srvlog',
  fetchEvents: (params, signal) => api.getSrvlogs(params, signal),
  useStream: useSrvlogStream,
  useFilterStore: useSrvlogFilterStore,
  matchesFilters,
})
