import { describe, it, expect, vi, beforeEach } from 'vitest'
import { createApp, nextTick, reactive } from 'vue'

// Mock vue-router before importing the factory. The route is reactive and
// replace() applies the query like a real history replace, so URL sync
// (syncToURL, the popstate watcher, and the `syncing` guard) is observable.
// A fresh route per test keeps watchers from earlier tests' stores (still
// live in their old pinia scopes) from reacting to this test's URL changes.
function makeRoute() {
  return reactive({ name: 'srvlog', query: {} as Record<string, string> })
}
const holder = { route: makeRoute() }
const replace = vi.fn((to: { query?: Record<string, string> }) => {
  holder.route.query = to.query ?? {}
  return Promise.resolve()
})
vi.mock('vue-router', () => ({
  useRoute: () => holder.route,
  useRouter: () => ({ replace }),
}))

import { setActivePinia, createPinia } from 'pinia'
import { createFilterStore } from '../filter-store-factory'

const filterKeys = ['hostname', 'severity', 'search'] as const

describe('createFilterStore', () => {
  beforeEach(() => {
    const app = createApp({})
    const pinia = createPinia()
    app.use(pinia)
    setActivePinia(pinia)
    holder.route = makeRoute()
    replace.mockClear()
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

  it('enforces mutually exclusive filter pairs (conflicts)', async () => {
    const useStore = createFilterStore('test-conflicts', filterKeys, 'srvlog', {
      conflicts: [['hostname', 'search']],
    })
    const store = useStore()
    store.filters.severity = '4' // non-conflicting key must survive throughout
    store.filters.hostname = 'router1'
    await nextTick()
    store.filters.search = 'error'
    await nextTick()
    expect(store.filters.hostname).toBe('') // setting search cleared hostname
    expect(store.filters.search).toBe('error')
    store.filters.hostname = 'router2'
    await nextTick()
    expect(store.filters.search).toBe('') // and the other direction
    expect(store.filters.hostname).toBe('router2')
    expect(store.filters.severity).toBe('4')
  })

  it('initFromURL seeds filters from the current query', () => {
    holder.route.query = { hostname: 'h1', bogus: 'x' }
    const useStore = createFilterStore('test-init-url', filterKeys, 'srvlog')
    const store = useStore()
    store.initFromURL()
    expect(store.filters.hostname).toBe('h1')
    expect(store.filters.severity).toBe('')
    expect(store.activeFilters).toEqual({ hostname: 'h1' })
  })

  it('syncs filters to the URL once per change, without a filter→URL→filter loop', async () => {
    const useStore = createFilterStore('test-sync-url', filterKeys, 'srvlog')
    const store = useStore()
    store.filters.hostname = 'server-01'
    await nextTick() // filters watcher → syncToURL → replace
    expect(replace).toHaveBeenCalledTimes(1)
    expect(replace).toHaveBeenCalledWith({ name: 'srvlog', query: { hostname: 'server-01' } })
    expect(holder.route.query).toEqual({ hostname: 'server-01' })
    // The reflected URL change must not echo back into another replace.
    await nextTick()
    await nextTick()
    expect(replace).toHaveBeenCalledTimes(1)
    expect(store.filters.hostname).toBe('server-01')
  })

  it('ignores query changes while a sync is in flight (syncing guard), then applies later ones', async () => {
    let resolveReplace!: () => void
    replace.mockImplementationOnce((to) => {
      holder.route.query = to.query ?? {}
      return new Promise<void>((resolve) => {
        resolveReplace = resolve
      })
    })
    const useStore = createFilterStore('test-syncing-guard', filterKeys, 'srvlog')
    const store = useStore()
    store.filters.hostname = 'mine'
    await nextTick() // syncToURL starts; replace stays pending, so syncing stays true
    holder.route.query = { hostname: 'external' } // lands mid-sync — must not be copied back
    await nextTick()
    expect(store.filters.hostname).toBe('mine')

    resolveReplace() // sync settles, syncing flag clears
    await nextTick()
    holder.route.query = { hostname: 'external2' } // popstate-style change now applies
    await nextTick()
    expect(store.filters.hostname).toBe('external2')
  })
})
