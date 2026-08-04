import { onMounted, onUnmounted, type Ref } from 'vue'

// Calls handler when a pointerdown lands outside the target element.
// Local replacement for @vueuse/core's onClickOutside.
export function useClickOutside(target: Ref<HTMLElement | null>, handler: () => void) {
  function onPointerDown(e: PointerEvent) {
    const el = target.value
    if (!el || e.composedPath().includes(el)) return
    handler()
  }

  onMounted(() => {
    document.addEventListener('pointerdown', onPointerDown)
  })
  onUnmounted(() => {
    document.removeEventListener('pointerdown', onPointerDown)
  })
}
