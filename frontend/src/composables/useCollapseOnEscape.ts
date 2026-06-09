import { ref, provide, onMounted, onUnmounted } from 'vue'

// Provides `collapseSignal` (incremented on each Escape keypress) so log-row
// components can collapse their expanded state. Shared by the device log views;
// rows consume it via inject<Ref<number>>('collapseSignal').
export function useCollapseOnEscape() {
  const collapseSignal = ref(0)
  provide('collapseSignal', collapseSignal)

  function onKeydown(e: KeyboardEvent) {
    if (e.code !== 'Escape') return
    collapseSignal.value++
  }

  onMounted(() => {
    document.addEventListener('keydown', onKeydown)
  })
  onUnmounted(() => {
    document.removeEventListener('keydown', onKeydown)
  })

  return { collapseSignal }
}
