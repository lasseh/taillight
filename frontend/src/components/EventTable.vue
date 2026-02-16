<script setup lang="ts" generic="T extends { id: number }">
import { ref, watch, nextTick, onMounted, onUnmounted, onActivated, onDeactivated } from 'vue'
import { useScrollStore } from '@/stores/scroll'
import LoadingIndicator from '@/components/LoadingIndicator.vue'
import ErrorDisplay from '@/components/ErrorDisplay.vue'

const props = defineProps<{
  routeName: string
  events: T[]
  loading: boolean
  error: string | null
  hasMore: boolean
  loadHistory: (reset: boolean, wrapMerge?: (mutate: () => void) => void) => void
}>()

const scrollStore = useScrollStore()
const scrollEl = ref<HTMLElement | null>(null)
const isPinned = ref(true)

function preserveScrollForPrepend(mutate: () => void) {
  const el = scrollEl.value
  if (!el) { mutate(); return }
  const prevHeight = el.scrollHeight
  const prevTop = el.scrollTop
  mutate()
  nextTick(() => {
    el.scrollTop = el.scrollHeight - prevHeight + prevTop
  })
}

function scrollToBottom(behavior: ScrollBehavior = 'smooth') {
  const el = scrollEl.value
  if (!el) return
  el.scrollTo({ top: el.scrollHeight, behavior })
  isPinned.value = true
  scrollStore.setPinned(props.routeName, true)
}

function onScroll() {
  const el = scrollEl.value
  if (!el) return
  isPinned.value = el.scrollHeight - el.scrollTop - el.clientHeight < 30
  scrollStore.setPinned(props.routeName, isPinned.value)

  // Infinite scroll: load more history when near the top.
  if (el.scrollTop < 200 && props.hasMore && !props.loading) {
    props.loadHistory(false, preserveScrollForPrepend)
  }
}

function onKeydown(e: KeyboardEvent) {
  if (e.code !== 'Space') return
  const tag = (e.target as HTMLElement)?.tagName
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || tag === 'BUTTON') return
  e.preventDefault()
  scrollToBottom()
}

onMounted(() => {
  document.addEventListener('keydown', onKeydown)
})

// Scroll to bottom whenever events arrive after being empty (initial load or filter reset).
let _prevEventCount = 0
watch(
  () => props.events.length,
  (len) => {
    if (len > 0 && _prevEventCount === 0) {
      isPinned.value = true
      scrollStore.setPinned(props.routeName, true)
      nextTick(() => scrollToBottom('instant'))
    }
    _prevEventCount = len
  },
)

onUnmounted(() => {
  document.removeEventListener('keydown', onKeydown)
})

onActivated(() => {
  if (scrollStore.consumeScrollToBottom(props.routeName)) {
    nextTick(() => scrollToBottom('instant'))
    return
  }
  const saved = scrollStore.getPosition(props.routeName)
  if (!saved) return
  if (saved.isPinned) {
    nextTick(() => scrollToBottom('instant'))
  } else {
    nextTick(() => {
      const el = scrollEl.value
      if (el) {
        el.scrollTop = saved.top
        isPinned.value = false
      }
    })
  }
})

onDeactivated(() => {
  const el = scrollEl.value
  if (el) {
    scrollStore.savePosition(props.routeName, el.scrollTop, isPinned.value)
  }
})

// Watch for jump-to-latest triggered from the status bar.
watch(
  () => scrollStore.getJumpSignal(props.routeName),
  () => scrollToBottom(),
)

// Handle scroll behavior when events change.
// - Pinned: auto-scroll to bottom.
// - Not pinned: preserve scroll position so the user's view stays stable
//   even when items are trimmed from the top of the buffer.
// Track the newest event ID so we can count new arrivals even when the
// buffer trims and the array length stays at MAX_EVENTS.
let _lastTailId = 0
watch(
  () => props.events,
  (evts) => {
    const el = scrollEl.value
    const last = evts[evts.length - 1]
    const tailId = last ? last.id : 0

    if (isPinned.value) {
      _lastTailId = tailId
      nextTick(() => {
        if (el) el.scrollTop = el.scrollHeight
      })
      return
    }

    // A new event was appended if the tail ID advanced.
    if (tailId > _lastTailId && _lastTailId > 0) {
      scrollStore.addNewEvents(props.routeName, 1)
    }
    _lastTailId = tailId

    // Preserve scroll position: capture pre-DOM height, adjust after render.
    if (el) {
      const prevHeight = el.scrollHeight
      const prevTop = el.scrollTop
      nextTick(() => {
        el.scrollTop = el.scrollHeight - prevHeight + prevTop
      })
    }
  },
  { flush: 'sync' },
)

// Intercept copy to produce clean log lines from selected rows.
function onCopy(e: ClipboardEvent) {
  const sel = window.getSelection()
  if (!sel || sel.isCollapsed) return

  const el = scrollEl.value
  if (!el) return

  const rows = el.querySelectorAll('[data-copytext]')
  const lines: string[] = []
  for (const row of rows) {
    if (sel.containsNode(row, true)) {
      const text = (row as HTMLElement).dataset.copytext
      if (text) lines.push(text)
    }
  }

  if (lines.length > 0) {
    e.preventDefault()
    e.clipboardData?.setData('text/plain', lines.join('\n'))
  }
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div class="relative flex-1 overflow-hidden">
      <LoadingIndicator v-if="loading" />

      <div v-if="error" class="px-4 py-4">
        <ErrorDisplay
          title="something went wrong"
          :message="error"
        />
      </div>

      <div v-if="events.length === 0 && !loading && !error" class="text-t-fg-dark px-4 py-4 text-center text-xs">
        no events
      </div>

      <div
        v-if="events.length > 0"
        ref="scrollEl"
        role="log"
        aria-live="polite"
        aria-label="Live event stream"
        class="h-full overflow-y-auto [overflow-anchor:none]"
        @scroll="onScroll"
        @copy="onCopy"
      >
        <div v-for="item in events" :key="item.id">
          <slot :item="item" />
        </div>
      </div>
    </div>

  </div>
</template>
