import { ref, watch, onUnmounted, type Ref } from 'vue'

// Tracks an element's content-box size via ResizeObserver.
// Local replacement for @vueuse/core's useElementSize.
export function useElementSize(target: Ref<HTMLElement | null>) {
  const width = ref(0)
  const height = ref(0)
  let observer: ResizeObserver | null = null

  watch(
    target,
    (el) => {
      observer?.disconnect()
      if (!el) {
        width.value = 0
        height.value = 0
        return
      }
      observer ??= new ResizeObserver((entries) => {
        const rect = entries[0]?.contentRect
        if (!rect) return
        width.value = rect.width
        height.value = rect.height
      })
      observer.observe(el)
    },
    { flush: 'post' },
  )

  onUnmounted(() => observer?.disconnect())

  return { width, height }
}
