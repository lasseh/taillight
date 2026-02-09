import type { SyslogEvent } from '@/types/syslog'
import { api } from '@/lib/api'
import { useSyslogStream } from '@/composables/useSyslogStream'
import { useSyslogFilterStore } from '@/stores/syslog-filters'
import { createEventStore } from '@/stores/event-store-factory'
import { wildcardMatch } from '@/lib/wildcard'

function matchesFilters(event: SyslogEvent, filters: Record<string, string>): boolean {
  if (filters.hostname) {
    if (filters.hostname.includes('*') ? !wildcardMatch(event.hostname, filters.hostname) : event.hostname !== filters.hostname) return false
  }
  if (filters.programname && event.programname !== filters.programname) return false
  if (filters.syslogtag && event.syslogtag !== filters.syslogtag) return false
  if (filters.facility && event.facility !== Number(filters.facility)) return false
  if (filters.severity_max && event.severity > Number(filters.severity_max)) return false
  if (filters.search && !event.message.toLowerCase().includes(filters.search.toLowerCase())) return false
  return true
}

export const useSyslogEventStore = createEventStore({
  id: 'syslog-events',
  routeName: 'syslog',
  fetchEvents: (params, signal) => api.getSyslogs(params, signal),
  useStream: useSyslogStream,
  useFilterStore: useSyslogFilterStore,
  matchesFilters,
})
