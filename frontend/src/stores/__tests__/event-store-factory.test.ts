import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ref } from 'vue'

// Mock vue-router before importing the factory.
vi.mock('vue-router', () => ({
  useRoute: () => ({ name: 'srvlog', query: {} }),
  useRouter: () => ({
    replace: vi.fn(() => Promise.resolve()),
  }),
}))

import { createApp } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import { createEventStore } from '../event-store-factory'
import { useScrollStore } from '../scroll'

interface TestEvent {
  id: number
  message: string
}

function makeStore(
  fetchEvents = vi.fn(() => Promise.resolve({ data: [] as TestEvent[], has_more: false })),
) {
  // Captures the SSE callback so tests can push events into the store.
  let emit: ((event: TestEvent) => void) | null = null
  const useStore = createEventStore<TestEvent>({
    id: `test-events-${Math.random()}`,
    routeName: 'srvlog',
    fetchEvents,
    useStream: () => ({
      connected: ref(true),
      reconnectAfterGap: ref(0),
      subscribe: (cb) => {
        emit = cb
        return () => {}
      },
    }),
    useFilterStore: () => ({ activeFilters: {} }),
    matchesFilters: () => true,
  })
  return { useStore, emit: (event: TestEvent) => emit!(event) }
}

describe('createEventStore', () => {
  beforeEach(() => {
    const app = createApp({})
    const pinia = createPinia()
    app.use(pinia)
    setActivePinia(pinia)
  })

  it('starts with empty events', () => {
    const { useStore } = makeStore()
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
    const fetchEvents = vi.fn(() => Promise.resolve({ data: events, has_more: false }))
    const { useStore } = makeStore(fetchEvents)
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

    const { useStore } = makeStore(fetchEvents)
    const store = useStore()

    await store.enter()
    expect(store.hasMore).toBe(true)

    await store.loadHistory()
    expect(store.events).toHaveLength(4)
    expect(store.events.map((e) => e.id)).toEqual([1, 2, 3, 4])
    expect(store.hasMore).toBe(false)
  })

  it('reset() drops buffered events and pagination state without refetching', async () => {
    const events = [
      { id: 2, message: 'newer' },
      { id: 1, message: 'older' },
    ]
    const fetchEvents = vi.fn(() => Promise.resolve({ data: events, cursor: 'c1', has_more: true }))
    const { useStore } = makeStore(fetchEvents)
    const store = useStore()

    await store.enter()
    expect(store.events).toHaveLength(2)
    expect(store.hasMore).toBe(true)

    store.reset()

    expect(store.events).toEqual([])
    expect(store.hasMore).toBe(false)
    expect(store.error).toBeNull()
    // No refetch on reset — only the enter() call hit the API.
    expect(fetchEvents).toHaveBeenCalledOnce()
  })

  it('caps SSE appends while detached and counts the rest as missed', async () => {
    const { useStore, emit } = makeStore()
    const store = useStore()
    const scrollStore = useScrollStore()

    await store.enter()
    scrollStore.setPinned('srvlog', false)

    for (let i = 1; i <= 600; i++) {
      emit({ id: i, message: `e${i}` })
    }

    // Only the first 500 append; the remaining 100 are dropped but counted.
    expect(store.events).toHaveLength(500)
    expect(scrollStore.getNewEventCount('srvlog')).toBe(100)
  })

  it('reattach() refetches when events were dropped while detached', async () => {
    const fetchEvents = vi.fn(() => Promise.resolve({ data: [] as TestEvent[], has_more: false }))
    const { useStore, emit } = makeStore(fetchEvents)
    const store = useStore()
    const scrollStore = useScrollStore()

    await store.enter()
    scrollStore.setPinned('srvlog', false)

    for (let i = 1; i <= 501; i++) {
      emit({ id: i, message: `e${i}` })
    }

    store.reattach()
    await vi.waitFor(() => expect(store.loading).toBe(false))

    // enter() ran again to close the gap left by the dropped event.
    expect(fetchEvents).toHaveBeenCalledTimes(2)
    expect(store.events).toEqual([])
  })

  it('clear() empties the buffer but keeps live streaming', async () => {
    const events = [
      { id: 2, message: 'newer' },
      { id: 1, message: 'older' },
    ]
    const fetchEvents = vi.fn(() => Promise.resolve({ data: events, cursor: 'c1', has_more: true }))
    const { useStore, emit } = makeStore(fetchEvents)
    const store = useStore()
    const scrollStore = useScrollStore()

    await store.enter()
    scrollStore.setPinned('srvlog', false)
    expect(store.events).toHaveLength(2)

    store.clear()

    expect(store.events).toEqual([])
    // Scroll-up must not refill history after a clear.
    expect(store.hasMore).toBe(false)
    // Re-pinned, so the pill counter is reset and the next event lands live.
    expect(scrollStore.isPinned('srvlog')).toBe(true)
    // No refetch — only the enter() call hit the API.
    expect(fetchEvents).toHaveBeenCalledOnce()

    // SSE keeps flowing into the emptied buffer.
    emit({ id: 3, message: 'after clear' })
    expect(store.events.map((e) => e.id)).toEqual([3])
  })

  it('reattach() only trims when nothing was dropped', async () => {
    const fetchEvents = vi.fn(() => Promise.resolve({ data: [] as TestEvent[], has_more: false }))
    const { useStore, emit } = makeStore(fetchEvents)
    const store = useStore()
    const scrollStore = useScrollStore()

    await store.enter()
    scrollStore.setPinned('srvlog', false)

    for (let i = 1; i <= 10; i++) {
      emit({ id: i, message: `e${i}` })
    }

    store.reattach()

    // Buffer kept (under MAX_EVENTS), no refetch beyond the initial enter().
    expect(store.events).toHaveLength(10)
    expect(fetchEvents).toHaveBeenCalledOnce()
  })
})
