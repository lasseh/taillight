import { ref, watch, nextTick, type Ref } from 'vue'

// Auto-scrolls a device log stream to the bottom while the user stays pinned
// there, and re-pins on tab switch. Identical behavior across the srvlog/netlog
// (DeviceLogView) and applog device views. Pass the scroll-container template
// ref (bound via ref="..."), the chronological (oldest-first) log list, and the
// active tab; wire onLogScroll to the container's @scroll.
export function useDeviceLogScroll(
  logScrollEl: Readonly<Ref<HTMLElement | null>>,
  chronologicalLogs: Readonly<Ref<readonly unknown[]>>,
  activeTab: Readonly<Ref<string>>,
) {
  const isPinned = ref(true)

  function scrollToBottom(behavior: ScrollBehavior = 'instant') {
    const el = logScrollEl.value
    if (!el) return
    el.scrollTo({ top: el.scrollHeight, behavior })
    isPinned.value = true
  }

  function onLogScroll() {
    const el = logScrollEl.value
    if (!el) return
    isPinned.value = el.scrollHeight - el.scrollTop - el.clientHeight < 30
  }

  // Auto-scroll to bottom when new events arrive (if pinned).
  watch(chronologicalLogs, () => {
    if (isPinned.value) {
      nextTick(() => scrollToBottom())
    }
  })

  // Scroll to bottom on tab switch.
  watch(activeTab, () => {
    isPinned.value = true
    nextTick(() => scrollToBottom())
  })

  return { isPinned, scrollToBottom, onLogScroll }
}
