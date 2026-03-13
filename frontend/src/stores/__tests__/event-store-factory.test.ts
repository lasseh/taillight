import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ref } from 'vue'

// Mock vue-router before importing the factory.
vi.mock('vue-router', () => ({
  useRoute: () => ({ name: 'syslog', query: {} }),
  useRouter: () => ({
    replace: vi.fn(() => Promise.resolve()),
  }),
}))

import { createApp } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import { createEventStore } from '../event-store-factory'

interface TestEvent {
  id: number
  message: string
}

function makeStore(
  fetchEvents = vi.fn(() =>
    Promise.resolve({ data: [] as TestEvent[], has_more: false }),
  ),
) {
  return createEventStore<TestEvent>({
    id: `test-events-${Math.random()}`,
    routeName: 'syslog',
    fetchEvents,
    useStream: () => ({
      connected: ref(true),
      subscribe: () => () => {},
    }),
    useFilterStore: () => ({ activeFilters: {} }),
    matchesFilters: () => true,
  })
}

describe('createEventStore', () => {
  beforeEach(() => {
    const app = createApp({})
    const pinia = createPinia()
    app.use(pinia)
    setActivePinia(pinia)
  })

  it('starts with empty events', () => {
    const useStore = makeStore()
    const store = useStore()
    expect(store.events).toEqual([])
    expect(store.loading).toBe(false)
    expect(store.error).toBeNull()
  })

  it('enter() calls fetchEvents and populates events', async () => {
    const events = [
      { id: 2, message: 'newer' },
      { id: 1, message: 'older' },
    ]
    const fetchEvents = vi.fn(() =>
      Promise.resolve({ data: events, has_more: false }),
    )
    const useStore = makeStore(fetchEvents)
    const store = useStore()

    await store.enter()

    expect(fetchEvents).toHaveBeenCalledOnce()
    // Events are reversed to chronological order.
    expect(store.events).toHaveLength(2)
    expect(store.events.map((e) => e.id)).toEqual([1, 2])
  })

  it('loadHistory() appends older events', async () => {
    const page1 = [
      { id: 4, message: 'd' },
      { id: 3, message: 'c' },
    ]
    const page2 = [
      { id: 2, message: 'b' },
      { id: 1, message: 'a' },
    ]

    let call = 0
    const fetchEvents = vi.fn(() => {
      call++
      if (call === 1) return Promise.resolve({ data: page1, cursor: 'c1', has_more: true })
      return Promise.resolve({ data: page2, has_more: false })
    })

    const useStore = makeStore(fetchEvents)
    const store = useStore()

    await store.enter()
    expect(store.hasMore).toBe(true)

    await store.loadHistory()
    expect(store.events).toHaveLength(4)
    expect(store.events.map((e) => e.id)).toEqual([1, 2, 3, 4])
    expect(store.hasMore).toBe(false)
  })
})
