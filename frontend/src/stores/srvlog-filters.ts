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

export const useSrvlogFilterStore = createFilterStore('srvlog-filters', FILTER_KEYS, 'srvlog', {
  conflicts: [['severity', 'severity_max']],
})
