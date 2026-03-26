import { createFilterStore } from '@/stores/filter-store-factory'

const FILTER_KEYS = [
  'from',
  'to',
  'hostname',
  'programname',
  'syslogtag',
  'facility',
  'severity',
  'severity_max',
  'search',
] as const

export const useNetlogFilterStore = createFilterStore('netlog-filters', FILTER_KEYS, 'netlog', {
  conflicts: [['severity', 'severity_max']],
})
