import { describe, it, expect, vi, beforeEach } from 'vitest'

// Mock vue-router before importing the factory.
vi.mock('vue-router', () => ({
  useRoute: () => ({ name: 'srvlog', query: {} }),
  useRouter: () => ({
    replace: vi.fn(() => Promise.resolve()),
  }),
}))

import { createApp } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import { createFilterStore } from '../filter-store-factory'

const filterKeys = ['hostname', 'severity', 'search'] as const

describe('createFilterStore', () => {
  beforeEach(() => {
    const app = createApp({})
    const pinia = createPinia()
    app.use(pinia)
    setActivePinia(pinia)
  })

  it('initializes all filters as empty strings', () => {
    const useStore = createFilterStore('test-filters', filterKeys, 'srvlog')
    const store = useStore()
    expect(store.filters.hostname).toBe('')
    expect(store.filters.severity).toBe('')
    expect(store.filters.search).toBe('')
  })

  it('computes activeFilters from non-empty values', () => {
    const useStore = createFilterStore('test-active', filterKeys, 'srvlog')
    const store = useStore()
    store.filters.hostname = 'server-01'
    expect(store.activeFilters).toEqual({ hostname: 'server-01' })
  })

  it('hasActiveFilters reflects filter state', () => {
    const useStore = createFilterStore('test-has', filterKeys, 'srvlog')
    const store = useStore()
    expect(store.hasActiveFilters).toBe(false)
    store.filters.search = 'error'
    expect(store.hasActiveFilters).toBe(true)
  })

  it('clearAll resets all filters', () => {
    const useStore = createFilterStore('test-clear', filterKeys, 'srvlog')
    const store = useStore()
    store.filters.hostname = 'a'
    store.filters.severity = 'b'
    store.filters.search = 'c'
    store.clearAll()
    expect(store.activeFilters).toEqual({})
  })
})
