import { ref } from 'vue'
import router from '@/router'

const LOG_ROUTES = new Set(['netlog', 'srvlog', 'applog'])

const active = ref(false)

function enter() {
  active.value = true
}

function exit() {
  active.value = false
}

function toggle() {
  active.value = !active.value
}

function isTextInput(el: Element | null): boolean {
  if (!el) return false
  const tag = el.tagName
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true
  if ((el as HTMLElement).isContentEditable) return true
  return false
}

function onKeydown(e: KeyboardEvent) {
  const routeName = router.currentRoute.value.name
  if (!routeName || !LOG_ROUTES.has(String(routeName))) return

  if (isTextInput(document.activeElement)) return
  if (e.ctrlKey || e.metaKey || e.altKey) return

  if (e.key === 'Escape' && active.value) {
    exit()
    e.stopPropagation()
    return
  }

  if (e.key === 'f') {
    toggle()
  }
}

// Capture phase so Escape is intercepted before EventTable's bubble-phase listener.
if (typeof document !== 'undefined') {
  document.addEventListener('keydown', onKeydown, true)
}

export function useFullscreen() {
  return { active, enter, exit, toggle }
}
