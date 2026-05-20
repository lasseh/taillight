import router from '@/router'

// Number keys map to the four main pages: 1=Dashboard, 2=Netlog, 3=Srvlog, 4=Applog.
const NAV_HOTKEYS: Record<string, string> = {
  '1': 'home',
  '2': 'netlog',
  '3': 'srvlog',
  '4': 'applog',
}

function isTextInput(el: Element | null): boolean {
  if (!el) return false
  const tag = el.tagName
  if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true
  if ((el as HTMLElement).isContentEditable) return true
  return false
}

function onKeydown(e: KeyboardEvent) {
  if (isTextInput(document.activeElement)) return
  if (e.ctrlKey || e.metaKey || e.altKey || e.shiftKey) return

  const target = NAV_HOTKEYS[e.key]
  if (!target) return

  // Don't fire a duplicate navigation to the page we're already on. The
  // current route may be a child of the target (e.g. srvlog-detail under
  // srvlog) — in that case we deliberately do navigate up to the feed.
  if (router.currentRoute.value.name === target) return

  e.preventDefault()
  void router.push({ name: target })
}

// Module-level registration (mirrors useFullscreen): the App mounts once, so
// one passive listener for the life of the page is enough.
if (typeof document !== 'undefined') {
  document.addEventListener('keydown', onKeydown)
}

export function useNavHotkeys() {
  // Side-effect only; expose nothing. Importing/calling makes the intent
  // explicit at the App.vue mount site.
}
