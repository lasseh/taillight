import { ref, type Ref, onScopeDispose } from 'vue'

const BOTTOM_THRESHOLD = 30

export function useAutoScroll(container: Ref<HTMLElement | null>) {
  const isPinned = ref(true)
  const isAtTop = ref(false)
  let rafId: number | null = null
  let resizeObserver: ResizeObserver | null = null
  let mutationObserver: MutationObserver | null = null
  let frozen = false

  function isAtBottom(el: HTMLElement): boolean {
    return el.scrollHeight - el.scrollTop - el.clientHeight < BOTTOM_THRESHOLD
  }

  function scrollToBottom(behavior: ScrollBehavior = 'smooth') {
    const el = container.value
    if (!el) return
    el.scrollTo({ top: el.scrollHeight, behavior })
    isPinned.value = true
  }

  function onScroll() {
    if (frozen) return
    const el = container.value
    if (!el) return
    isPinned.value = isAtBottom(el)
    isAtTop.value = el.scrollTop < 5
  }

  function onNewEvent() {
    if (frozen) return
    if (!isPinned.value) return
    if (rafId !== null) return
    rafId = requestAnimationFrame(() => {
      rafId = null
      const el = container.value
      if (!el) return
      el.scrollTop = el.scrollHeight
    })
  }

  function preserveScrollForPrepend(mutate: () => void) {
    const el = container.value
    if (!el) {
      mutate()
      return
    }
    const prevHeight = el.scrollHeight
    mutate()
    requestAnimationFrame(() => {
      const delta = el.scrollHeight - prevHeight
      el.scrollTop += delta
    })
  }

  function scrollIfPinned() {
    if (frozen) return
    if (isPinned.value) {
      const el = container.value
      if (el) el.scrollTop = el.scrollHeight
    }
  }

  /** Suppress all auto-scroll (scroll handler, observers, onNewEvent). */
  function freeze() {
    frozen = true
  }

  /** Re-enable auto-scroll and re-evaluate isPinned from actual position. */
  function unfreeze() {
    frozen = false
    const el = container.value
    if (el) {
      isPinned.value = isAtBottom(el)
    }
  }

  function attach() {
    const el = container.value
    if (!el) return
    el.addEventListener('scroll', onScroll, { passive: true })

    // Observe children for size changes (e.g. expanding EventDetail rows).
    resizeObserver = new ResizeObserver(scrollIfPinned)
    for (const child of el.children) {
      resizeObserver.observe(child)
    }

    // Watch for added/removed children so new rows get observed too.
    mutationObserver = new MutationObserver((mutations) => {
      for (const m of mutations) {
        for (const node of m.addedNodes) {
          if (node instanceof HTMLElement) resizeObserver?.observe(node)
        }
      }
    })
    mutationObserver.observe(el, { childList: true })
  }

  function detach() {
    const el = container.value
    if (el) {
      el.removeEventListener('scroll', onScroll)
    }
    if (resizeObserver) {
      resizeObserver.disconnect()
      resizeObserver = null
    }
    if (mutationObserver) {
      mutationObserver.disconnect()
      mutationObserver = null
    }
    if (rafId !== null) {
      cancelAnimationFrame(rafId)
      rafId = null
    }
  }

  onScopeDispose(detach)

  return {
    isPinned,
    isAtTop,
    scrollToBottom,
    onNewEvent,
    preserveScrollForPrepend,
    attach,
    detach,
    freeze,
    unfreeze,
  }
}
