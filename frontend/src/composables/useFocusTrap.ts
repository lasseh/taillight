import { watch, onUnmounted, type Ref } from 'vue'
import { createFocusTrap, type FocusTrap } from 'focus-trap'

/** Activate a focus trap while `el` is non-null. Cleans up on unmount. */
export function useFocusTrap(el: Ref<HTMLElement | null>) {
  let trap: FocusTrap | null = null

  watch(el, (node) => {
    trap?.deactivate()
    trap = null
    if (node) {
      trap = createFocusTrap(node, {
        escapeDeactivates: false,
        allowOutsideClick: true,
      })
      trap.activate()
    }
  })

  onUnmounted(() => {
    trap?.deactivate()
    trap = null
  })
}
