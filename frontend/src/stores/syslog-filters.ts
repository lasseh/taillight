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

export const useSyslogFilterStore = createFilterStore('syslog-filters', FILTER_KEYS, 'syslog', {
  conflicts: [['severity', 'severity_max']],
})
