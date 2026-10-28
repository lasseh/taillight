import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ref, type Ref } from 'vue'

// Mock vue-router before importing the factory.
vi.mock('vue-router', () => ({
  useRoute: () => ({ name: 'srvlog', query: {} }),
  useRouter: () => ({
    replace: vi.fn(() => Promise.resolve()),
  }),
}))

import { createApp } from 'vue'
import { flushPromises } from '@vue/test-utils'
import { setActivePinia, createPinia } from 'pinia'
import { createEventStore } from '../event-store-factory'
import { useScrollStore } from '../scroll'

interface TestEvent {
  id: number
  message: string
  muted_by?: number
}

type Page = { data: TestEvent[]; cursor?: string; has_more: boolean }

function makeStore(
  fetchEvents: (params: URLSearchParams, signal?: AbortSignal) => Promise<Page> = vi.fn(() =>
    Promise.resolve({ data: [] as TestEvent[], has_more: false }),
  ),
  showMuted?: Ref<boolean>,
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
    ...(showMuted && { showMuted: () => showMuted }),
  })
  return { useStore, emit: (event: TestEvent) => emit!(event) }
}

// A fetchEvents whose calls stay pending until the test resolves them. It
// ignores the abort signal, like a response already in hand when the abort
// came.
function deferredFetch() {
  const calls: {
    params: URLSearchParams
    signal: AbortSignal | undefined
    resolve: (p: Page) => void
  }[] = []
  const fetchEvents = vi.fn(
    (params: URLSearchParams, signal?: AbortSignal) =>
      new Promise<Page>((resolve) => calls.push({ params, signal, resolve })),
  )
  return { calls, fetchEvents }
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

  it('reset() during a load discards the late response and clears loading', async () => {
    const { calls, fetchEvents } = deferredFetch()
    const { useStore } = makeStore(fetchEvents)
    const store = useStore()

    void store.enter()
    store.reset()
    calls[0]!.resolve({ data: [{ id: 1, message: 'late' }], has_more: false })
    await flushPromises()

    expect(store.events).toEqual([])
    expect(store.loading).toBe(false)
  })

  it('clear() during the first load discards the late response and keeps live streaming', async () => {
    const { calls, fetchEvents } = deferredFetch()
    const { useStore, emit } = makeStore(fetchEvents)
    const store = useStore()

    void store.enter()
    store.clear()
    calls[0]!.resolve({ data: [{ id: 1, message: 'late' }], has_more: false })
    await flushPromises()
    expect(store.events).toEqual([])

    emit({ id: 2, message: 'live' })
    expect(store.events.map((e) => e.id)).toEqual([2])
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

  describe('muted events', () => {
    it('never hides without a showMuted input', async () => {
      const fetchEvents = vi.fn<(params: URLSearchParams) => Promise<Page>>(() =>
        Promise.resolve({ data: [{ id: 2, message: 'muted', muted_by: 5 }], has_more: false }),
      )
      const { useStore, emit } = makeStore(fetchEvents)
      const store = useStore()

      await store.enter()
      emit({ id: 1, message: 'muted', muted_by: 5 })

      expect(fetchEvents.mock.calls[0]![0].has('hide_muted')).toBe(false)
      expect(store.events.map((e) => e.id)).toEqual([2, 1])
      expect(store.hiddenMutedCount).toBe(0)
    })

    it('sends hide_muted=true while hiding and omits it after toggling to show', async () => {
      const fetchEvents = vi.fn<(params: URLSearchParams) => Promise<Page>>(() =>
        Promise.resolve({ data: [], cursor: 'c1', has_more: true }),
      )
      const { useStore } = makeStore(fetchEvents, ref(false))
      const store = useStore()

      await store.enter()
      expect(fetchEvents.mock.calls[0]![0].get('hide_muted')).toBe('true')
      // Scrollback pages hide too.
      await store.loadHistory()
      expect(fetchEvents.mock.calls[1]![0].get('hide_muted')).toBe('true')

      store.toggleMuted()
      await flushPromises()
      expect(fetchEvents).toHaveBeenCalledTimes(3)
      expect(fetchEvents.mock.calls[2]![0].has('hide_muted')).toBe(false)
    })

    it('drops a muted event while hiding and counts it once, also when re-emitted', async () => {
      const { useStore, emit } = makeStore(undefined, ref(false))
      const store = useStore()

      await store.enter()
      emit({ id: 1, message: 'muted', muted_by: 5 })
      emit({ id: 1, message: 'muted', muted_by: 5 })
      emit({ id: 2, message: 'plain' })

      expect(store.events.map((e) => e.id)).toEqual([2])
      expect(store.hiddenMutedCount).toBe(1)
    })

    it('appends a muted event while showing and leaves the count at 0', async () => {
      const { useStore, emit } = makeStore(undefined, ref(true))
      const store = useStore()

      await store.enter()
      emit({ id: 1, message: 'muted', muted_by: 5 })

      expect(store.events.map((e) => e.id)).toEqual([1])
      expect(store.hiddenMutedCount).toBe(0)
    })

    it('keeps muted drops out of the detached buffer, "new" counter and append limit', async () => {
      const { useStore, emit } = makeStore(undefined, ref(false))
      const store = useStore()
      const scrollStore = useScrollStore()

      await store.enter()
      scrollStore.setPinned('srvlog', false)

      // More muted events than DETACHED_SSE_APPEND_LIMIT (500).
      for (let i = 1; i <= 600; i++) {
        emit({ id: i, message: `m${i}`, muted_by: 5 })
      }
      expect(store.events).toEqual([])
      expect(scrollStore.getNewEventCount('srvlog')).toBe(0)
      expect(store.hiddenMutedCount).toBe(600)

      // The append allowance is untouched, so an unmuted event still lands.
      emit({ id: 601, message: 'plain' })
      expect(store.events.map((e) => e.id)).toEqual([601])
    })

    it('resets the count on clear, enter and reset', async () => {
      const { useStore, emit } = makeStore(undefined, ref(false))
      const store = useStore()
      await store.enter()

      emit({ id: 1, message: 'muted', muted_by: 5 })
      expect(store.hiddenMutedCount).toBe(1)
      store.clear()
      expect(store.hiddenMutedCount).toBe(0)

      emit({ id: 2, message: 'muted', muted_by: 5 })
      expect(store.hiddenMutedCount).toBe(1)
      await store.enter()
      expect(store.hiddenMutedCount).toBe(0)

      emit({ id: 3, message: 'muted', muted_by: 5 })
      expect(store.hiddenMutedCount).toBe(1)
      store.reset()
      expect(store.hiddenMutedCount).toBe(0)
    })

    it('drops marked rows from a hide-mode page without counting them', async () => {
      const page = [
        { id: 3, message: 'plain' },
        { id: 2, message: 'drifted past the SQL exclusion', muted_by: 5 },
        { id: 1, message: 'plain' },
      ]
      const fetchEvents = vi.fn(() => Promise.resolve({ data: page, has_more: false }))
      const { useStore, emit } = makeStore(fetchEvents, ref(false))
      const store = useStore()

      await store.enter()
      expect(store.events.map((e) => e.id)).toEqual([1, 3])
      expect(store.hiddenMutedCount).toBe(0)

      // The dropped id is still known, so its SSE copy is deduped rather
      // than counted as a live hide.
      emit({ id: 2, message: 'drifted past the SQL exclusion', muted_by: 5 })
      expect(store.hiddenMutedCount).toBe(0)
    })

    it('a toggle to hide during a load refetches in hide mode and drops the stale page', async () => {
      const { calls, fetchEvents } = deferredFetch()
      const { useStore, emit } = makeStore(fetchEvents, ref(true))
      const store = useStore()

      void store.enter()
      store.toggleMuted()
      await flushPromises()

      // The toggle superseded the in-flight load instead of being swallowed.
      expect(calls).toHaveLength(2)
      expect(calls[0]!.signal?.aborted).toBe(true)
      expect(calls[1]!.params.get('hide_muted')).toBe('true')

      // The show-mode page lands late, carrying a muted row.
      calls[0]!.resolve({ data: [{ id: 1, message: 'muted', muted_by: 5 }], has_more: false })
      await flushPromises()
      expect(store.loading).toBe(true)
      expect(store.events).toEqual([])
      // Live events wait for the current load, as on any first load.
      emit({ id: 3, message: 'live' })
      expect(store.events).toEqual([])

      calls[1]!.resolve({ data: [{ id: 2, message: 'plain' }], has_more: false })
      await flushPromises()
      expect(store.loading).toBe(false)
      expect(store.events.map((e) => e.id)).toEqual([2])
    })

    it('a toggle to show during a load ends with the show-mode page, even if the stale one lands last', async () => {
      const { calls, fetchEvents } = deferredFetch()
      const { useStore } = makeStore(fetchEvents, ref(false))
      const store = useStore()

      void store.enter()
      store.toggleMuted()
      await flushPromises()

      expect(calls).toHaveLength(2)
      expect(calls[0]!.params.get('hide_muted')).toBe('true')
      expect(calls[1]!.params.has('hide_muted')).toBe(false)

      calls[1]!.resolve({
        data: [
          { id: 2, message: 'muted', muted_by: 5 },
          { id: 1, message: 'plain' },
        ],
        has_more: false,
      })
      await flushPromises()
      calls[0]!.resolve({ data: [{ id: 1, message: 'plain' }], has_more: false })
      await flushPromises()

      expect(store.loading).toBe(false)
      expect(store.events).toEqual([
        { id: 1, message: 'plain' },
        { id: 2, message: 'muted', muted_by: 5 },
      ])
    })

    it('toggleMuted() flips the show state and refetches', async () => {
      const fetchEvents = vi.fn(() => Promise.resolve({ data: [] as TestEvent[], has_more: false }))
      const showMuted = ref(false)
      const { useStore } = makeStore(fetchEvents, showMuted)
      const store = useStore()
      await store.enter()

      store.toggleMuted()
      await flushPromises()

      expect(store.showMuted).toBe(true)
      expect(showMuted.value).toBe(true)
      expect(fetchEvents).toHaveBeenCalledTimes(2)
    })
  })
})
