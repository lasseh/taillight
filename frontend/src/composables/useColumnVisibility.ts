import { ref, watch } from 'vue'
import type { Ref } from 'vue'

/**
 * Per-route column visibility persisted to localStorage.
 * Key format: `taillight.<route>.<column>.visible`.
 */

const STORAGE_PREFIX = 'taillight'

const cache = new Map<string, Ref<boolean>>()

function storageKey(route: string, column: string): string {
  return `${STORAGE_PREFIX}.${route}.${column}.visible`
}

function readInitial(key: string, fallback: boolean): boolean {
  try {
    const raw = localStorage.getItem(key)
    if (raw === null) return fallback
    return raw === 'true'
  } catch {
    return fallback
  }
}

export function useColumnVisibility(route: string, column: string, defaultVisible = true) {
  const key = storageKey(route, column)
  let visible = cache.get(key)
  if (!visible) {
    visible = ref(readInitial(key, defaultVisible))
    watch(visible, (v) => {
      try {
        localStorage.setItem(key, String(v))
      } catch {
        // storage may be unavailable (private mode, quota); ignore.
      }
    })
    cache.set(key, visible)
  }

  function toggle() {
    visible!.value = !visible!.value
  }

  function show() {
    visible!.value = true
  }

  function hide() {
    visible!.value = false
  }

  return { visible, toggle, show, hide }
}
