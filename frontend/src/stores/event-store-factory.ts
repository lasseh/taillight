import { ref, shallowRef, computed, watch, onScopeDispose, type Ref } from 'vue'
import { defineStore } from 'pinia'
import { useRoute } from 'vue-router'
import { useScrollStore } from '@/stores/scroll'

// Buffer cap while pinned (live-tail mode). The browser holds ~MAX_EVENTS
// rows during normal operation, keeping the DOM cheap and scroll snappy.
const MAX_EVENTS = 2000

// Buffer cap while unpinned (deliberate scrollback / investigation mode).
// The buffer is allowed to grow beyond MAX_EVENTS so paging back preserves
// both the historical context AND the live tail accumulating below. Hitting
// this ceiling parks hasMore at false; the user can scroll up to read what's
// loaded but cannot fetch more older pages without first re-attaching to
// live (Esc / jump-to-latest / organic scroll-to-bottom), which trims the
// buffer back to MAX_EVENTS.
const MAX_EVENTS_DETACHED = 20_000

interface EventStoreConfig<TEvent extends { id: number }> {
  /** Pinia store identifier. */
  id: string
  /** Route name to watch for filter changes. */
  routeName: string
  /** Fetch a page of events from the API. */
  fetchEvents: (params: URLSearchParams, signal?: AbortSignal) => Promise<{ data: TEvent[]; cursor?: string; has_more: boolean }>
  /** Get the SSE stream composable. */
  useStream: () => {
    connected: Ref<boolean>
    /** Bumped when the stream reconnects after an outage long enough to outrun the server's SSE backfill window. */
    reconnectAfterGap: Ref<number>
    subscribe: (cb: (event: TEvent) => void) => () => void
  }
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
    // True when the buffer is at MAX_EVENTS_DETACHED during scrollback and we
    // refuse to fetch more older pages. The user must re-attach to live to
    // reset. Distinct from hasMore=false-because-server-has-no-more.
    const atCap = ref(false)

    // SSE deduplication.
    const _knownIds = new Set<number>()
    const _initialLoadComplete = ref(false)

    // Subscribe to the global SSE stream.
    const stream = config.useStream()

    const filterStore = config.useFilterStore()
    const scrollStore = useScrollStore()

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
      if (scrollStore.isPinned(config.routeName)) {
        // Live-tail: append with cap, oldest trimmed.
        const next = [...events.value, event]
        events.value = next.length > MAX_EVENTS ? next.slice(-MAX_EVENTS) : next
      } else if (!atCap.value) {
        // Scrollback mode: append without trimming so the user keeps both
        // their historical context and the live tail accumulating below.
        events.value = [...events.value, event]
        if (events.value.length >= MAX_EVENTS_DETACHED) {
          atCap.value = true
          hasMore.value = false
        }
      } else {
        // Cap reached and unpinned: drop the event from the buffer but keep
        // the counter ticking so the user knows how many they've missed.
        // EventTable's watch on events.value can't see this since we don't
        // mutate, so increment directly.
        scrollStore.addNewEvents(config.routeName, 1)
      }
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
        atCap.value = false
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
          events.value = reversed.length > MAX_EVENTS ? reversed.slice(-MAX_EVENTS) : reversed
        } else {
          const merge = () => {
            // Scrollback mode: prepend without trimming. The buffer holds the
            // complete chronological window from the oldest paged-back event
            // through the live tail. Re-attach to live (reattach()) trims it
            // back to MAX_EVENTS.
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
        // Park hasMore at false once the scrollback buffer reaches its
        // ceiling; the user must re-attach to live (which trims) before
        // fetching more older pages.
        if (events.value.length >= MAX_EVENTS_DETACHED) {
          atCap.value = true
          hasMore.value = false
        } else {
          hasMore.value = res.has_more
        }
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
      atCap.value = false
      _knownIds.clear()
      _initialLoadComplete.value = false

      await loadHistory(true)
      _initialLoadComplete.value = true
    }

    /**
     * Trim the scrollback buffer back to MAX_EVENTS and reset pagination
     * state. Called on the rising edge of isPinned (Esc, jump-to-latest,
     * organic scroll-to-bottom). The cursor is nulled because it points to
     * events older than the buffer's previous top, which after trimming is
     * no longer adjacent to anything in the buffer — the next paginate-back
     * will fetch the latest 100 with no cursor and use the response's fresh
     * cursor for subsequent pages.
     */
    function reattach() {
      if (events.value.length > MAX_EVENTS) {
        events.value = events.value.slice(-MAX_EVENTS)
      }
      cursor.value = null
      hasMore.value = true
      atCap.value = false
      _knownIds.clear()
      for (const e of events.value) _knownIds.add(e.id)
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

    // After a long SSE outage the server's 100-event backfill leaves an
    // invisible gap between backfill and live events. Refresh the view from
    // REST so the user sees the actual recent history. Only refresh when this
    // store's view is active; other routes will refresh via their own enter()
    // on activation.
    const _stopReconnectWatch = watch(
      () => stream.reconnectAfterGap.value,
      (count, prev) => {
        if (count === prev) return
        if (!_initialLoadComplete.value) return
        if (route.name !== config.routeName) return
        enter()
      },
    )

    // These event stores are app-lifetime Pinia singletons (single createApp +
    // createPinia, no SSR). onScopeDispose therefore runs only on Pinia/app
    // teardown (HMR, tests, app unmount) — never on route leave — so the stream
    // subscription and watchers are intentionally process-lifetime, matching the
    // "SSE stays connected across route changes" design. This is teardown for
    // disposal, not per-route cleanup.
    onScopeDispose(() => {
      _unsubscribe()
      _stopFilterWatch()
      _stopReconnectWatch()
    })

    return {
      events,
      loading,
      error,
      hasMore,
      atCap,
      connected: stream.connected,
      enter,
      loadHistory,
      reattach,
    }
  })
}
