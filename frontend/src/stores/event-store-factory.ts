import { ref, shallowRef, computed, watch, onScopeDispose, type Ref } from 'vue'
import { defineStore } from 'pinia'
import { useRoute } from 'vue-router'

const MAX_EVENTS = 2000

interface EventStoreConfig<TEvent extends { id: number }> {
  /** Pinia store identifier. */
  id: string
  /** Route name to watch for filter changes. */
  routeName: string
  /** Fetch a page of events from the API. */
  fetchEvents: (params: URLSearchParams, signal?: AbortSignal) => Promise<{ data: TEvent[]; cursor?: string; has_more: boolean }>
  /** Get the SSE stream composable. */
  useStream: () => { connected: Ref<boolean>; subscribe: (cb: (event: TEvent) => void) => () => void }
  /** Get the filter store — must return an object with activeFilters (unwrapped by Pinia). */
  useFilterStore: () => { activeFilters: Record<string, string> }
  /** Client-side filter for SSE events. */
  matchesFilters: (event: TEvent, filters: Record<string, string>) => boolean
}

/**
 * Creates a Pinia event store with SSE subscription, deduplication, and cursor-based pagination.
 */
export function createEventStore<TEvent extends { id: number }>(
  config: EventStoreConfig<TEvent>,
) {
  return defineStore(config.id, () => {
    const route = useRoute()

    const events = shallowRef<TEvent[]>([])
    const loading = ref(false)
    const error = ref<string | null>(null)

    // History pagination.
    const cursor = ref<string | null>(null)
    const hasMore = ref(false)

    // SSE deduplication.
    const _knownIds = new Set<number>()
    const _initialLoadComplete = ref(false)

    // Subscribe to the global SSE stream.
    const stream = config.useStream()

    const filterStore = config.useFilterStore()

    // Wrap the Pinia-unwrapped activeFilters in a computed for reactivity.
    const activeFilters = computed(() => filterStore.activeFilters)

    const _unsubscribe = stream.subscribe((event) => {
      if (!_initialLoadComplete.value) return
      if (_knownIds.has(event.id)) return
      if (!config.matchesFilters(event, activeFilters.value)) return
      _knownIds.add(event.id)
      // Trim oldest 5k IDs when set exceeds 10k to prevent unbounded memory
      // growth during long-lived sessions.
      if (_knownIds.size > 10000) {
        const iter = _knownIds.values()
        for (let i = 0; i < 5000; i++) {
          _knownIds.delete(iter.next().value!)
        }
      }
      const next = [...events.value, event]
      events.value = next.length > MAX_EVENTS ? next.slice(-MAX_EVENTS) : next
    })

    let _abortController: AbortController | null = null

    async function loadHistory(reset = false, wrapMerge?: (mutate: () => void) => void) {
      if (loading.value) return
      if (!reset && !hasMore.value) return

      if (_abortController) {
        _abortController.abort()
      }
      _abortController = new AbortController()
      const signal = _abortController.signal

      loading.value = true
      error.value = null

      if (reset) {
        events.value = []
        cursor.value = null
        _knownIds.clear()
      }

      try {
        const params = new URLSearchParams(activeFilters.value)
        params.set('limit', '100')
        if (cursor.value) {
          params.set('cursor', cursor.value)
        }

        const res = await config.fetchEvents(params, signal)
        // API returns events in DESC order; reverse to chronological for display.
        const reversed = [...res.data].reverse()

        for (const e of reversed) {
          _knownIds.add(e.id)
        }

        if (reset) {
          events.value = reversed
        } else {
          const merge = () => {
            events.value = [...reversed, ...events.value]
          }
          // wrapMerge lets the caller preserve scroll position when prepending
          // older events (see EventTable.preserveScrollForPrepend).
          if (wrapMerge) {
            wrapMerge(merge)
          } else {
            merge()
          }
        }

        cursor.value = res.cursor ?? null
        hasMore.value = res.has_more
      } catch (e) {
        if (signal.aborted) return
        error.value = e instanceof Error ? e.message : 'failed to load events'
      } finally {
        loading.value = false
      }
    }

    /** Called when the list view activates for the first time. */
    async function enter() {
      events.value = []
      cursor.value = null
      hasMore.value = false
      _knownIds.clear()
      _initialLoadComplete.value = false

      await loadHistory(true)
      _initialLoadComplete.value = true
    }

    // Reconnect / refetch when filters change.
    const _stopFilterWatch = watch(
      activeFilters,
      () => {
        if (route.name === config.routeName) {
          enter()
        }
      },
      { deep: true },
    )

    onScopeDispose(() => {
      _unsubscribe()
      _stopFilterWatch()
    })

    return {
      events,
      loading,
      error,
      hasMore,
      connected: stream.connected,
      enter,
      loadHistory,
    }
  })
}
