import { ref, computed } from 'vue'

const STORAGE_KEY = 'taillight-dashboard-hidden'

function loadHidden(): Set<string> {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw) return new Set(JSON.parse(raw) as string[])
  } catch { /* ignore corrupt data */ }
  return new Set()
}

function saveHidden(ids: Set<string>) {
  localStorage.setItem(STORAGE_KEY, JSON.stringify([...ids]))
}

const hiddenWidgets = ref(loadHidden())
const editing = ref(false)

export function useDashboardLayout() {
  function isVisible(id: string): boolean {
    return !hiddenWidgets.value.has(id)
  }

  function hideWidget(id: string) {
    hiddenWidgets.value = new Set([...hiddenWidgets.value, id])
  }

  function startEditing() {
    editing.value = true
  }

  function stopEditing() {
    editing.value = false
    saveHidden(hiddenWidgets.value)
  }

  function resetLayout() {
    hiddenWidgets.value = new Set()
    saveHidden(hiddenWidgets.value)
  }

  const allHidden = computed(() => hiddenWidgets.value.size >= 12)

  return { editing, hiddenWidgets, isVisible, hideWidget, startEditing, stopEditing, resetLayout, allHidden }
}
