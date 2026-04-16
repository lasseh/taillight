import { ref, watch, type Ref } from 'vue'

export function useCollapsed(key: string, initial = false): Ref<boolean> {
  const storageKey = `taillight-collapsed:${key}`
  let start = initial
  try {
    const raw = localStorage.getItem(storageKey)
    if (raw !== null) start = raw === '1'
  } catch { /* ignore corrupt data */ }

  const state = ref(start)
  watch(state, (v) => {
    try {
      localStorage.setItem(storageKey, v ? '1' : '0')
    } catch { /* ignore quota errors */ }
  })
  return state
}
