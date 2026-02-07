import { ref, computed } from 'vue'
import { themes } from '@/lib/themes'

const STORAGE_KEY = 'taillight-theme'
const DEFAULT_THEME = 'tokyonight'

const themeId = ref(localStorage.getItem(STORAGE_KEY) ?? DEFAULT_THEME)

// Apply theme on module load (covers SPA navigation)
if (typeof document !== 'undefined') applyTheme(themeId.value)

function applyTheme(id: string) {
  if (id === DEFAULT_THEME) {
    delete document.documentElement.dataset.theme
  } else {
    document.documentElement.dataset.theme = id
  }
}

function setTheme(id: string) {
  themeId.value = id
  localStorage.setItem(STORAGE_KEY, id)
  applyTheme(id)
}

const current = computed(() => {
  return themes.find((t) => t.id === themeId.value) ?? themes[0]!
})

export function useTheme() {
  return { themes, current, themeId, setTheme }
}
