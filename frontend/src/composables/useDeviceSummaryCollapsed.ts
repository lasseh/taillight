import { ref, watch, type Ref } from 'vue'

const STORAGE_KEY = 'taillight:device-summary-collapsed'

function load(): boolean {
  try {
    return localStorage.getItem(STORAGE_KEY) === '1'
  } catch {
    return false
  }
}

const state = ref(load())

watch(state, (v) => {
  try {
    localStorage.setItem(STORAGE_KEY, v ? '1' : '0')
  } catch {
    /* ignore quota errors */
  }
})

export function useDeviceSummaryCollapsed(): Ref<boolean> {
  return state
}
