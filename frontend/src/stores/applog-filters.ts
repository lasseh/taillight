import { createFilterStore } from '@/stores/filter-store-factory'

const FILTER_KEYS = [
  'from',
  'to',
  'service',
  'component',
  'host',
  'level',
  'search',
] as const

export const useAppLogFilterStore = createFilterStore('applog-filters', FILTER_KEYS, 'applog')
