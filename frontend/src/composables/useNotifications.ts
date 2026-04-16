import { ref } from 'vue'
import router from '@/router'
import { useAuthStore } from '@/stores/auth'
import type { SrvlogEvent } from '@/types/srvlog'
import type { NetlogEvent } from '@/types/netlog'
import type { AppLogEvent } from '@/types/applog'
import type { BrowserNotificationSettings } from '@/types/auth'
import { severityLabels } from '@/lib/constants'

/** Default severity threshold — emerg(0), alert(1), crit(2). */
const DEFAULT_MAX_SEVERITY = 2

/** Default applog levels that trigger browser push. */
const DEFAULT_APPLOG_LEVELS = ['ERROR', 'FATAL']

/** Ignore backfilled events older than this (ms). */
const MAX_AGE_MS = 30_000

const STORAGE_KEY = 'taillight-notifications'

const permission = ref<NotificationPermission>(
  typeof Notification !== 'undefined' ? Notification.permission : 'denied',
)

const supported = typeof Notification !== 'undefined'

const enabled = ref<boolean>(localStorage.getItem(STORAGE_KEY) !== 'off')

async function requestPermission() {
  if (!supported) return
  permission.value = await Notification.requestPermission()
}

function setEnabled(value: boolean) {
  enabled.value = value
  localStorage.setItem(STORAGE_KEY, value ? 'on' : 'off')
}

function isTooOld(receivedAt: string): boolean {
  return Date.now() - new Date(receivedAt).getTime() > MAX_AGE_MS
}

function getPrefs(): BrowserNotificationSettings | undefined {
  const auth = useAuthStore()
  return auth.user?.preferences?.browser_notifications
}

function notifySrvlog(event: SrvlogEvent) {
  if (!supported) return
  if (!enabled.value) return
  if (permission.value !== 'granted') return
  if (isTooOld(event.received_at)) return

  const prefs = getPrefs()
  if (prefs?.srvlog?.enabled === false) return
  const maxSev = prefs?.srvlog?.max_severity ?? DEFAULT_MAX_SEVERITY
  if (event.severity > maxSev) return

  const level = severityLabels[event.severity] ?? 'unknown'
  const title = `${event.hostname} - ${level}`
  const body = event.message.slice(0, 120)

  const n = new Notification(title, {
    body,
    tag: `srvlog-${event.id}`,
  })
  n.onclick = () => {
    window.focus()
    router.push(`/srvlog/${event.id}`)
    n.close()
  }
}

function notifyNetlog(event: NetlogEvent) {
  if (!supported) return
  if (!enabled.value) return
  if (permission.value !== 'granted') return
  if (isTooOld(event.received_at)) return

  const prefs = getPrefs()
  // Netlog defaults to enabled.
  if (prefs?.netlog?.enabled === false) return
  const maxSev = prefs?.netlog?.max_severity ?? DEFAULT_MAX_SEVERITY
  if (event.severity > maxSev) return

  const level = severityLabels[event.severity] ?? 'unknown'
  const title = `${event.hostname} - ${level}`
  const body = event.message.slice(0, 120)

  const n = new Notification(title, {
    body,
    tag: `netlog-${event.id}`,
  })
  n.onclick = () => {
    window.focus()
    router.push(`/netlog/${event.id}`)
    n.close()
  }
}

function notifyApplog(event: AppLogEvent) {
  if (!supported) return
  if (!enabled.value) return
  if (permission.value !== 'granted') return
  if (isTooOld(event.received_at)) return

  const prefs = getPrefs()
  if (prefs?.applog?.enabled === false) return
  const levels = new Set(prefs?.applog?.levels ?? DEFAULT_APPLOG_LEVELS)
  if (!levels.has(event.level)) return

  const title = `${event.service} - ${event.level}`
  const body = event.msg.slice(0, 120)

  const n = new Notification(title, {
    body,
    tag: `applog-${event.id}`,
  })
  n.onclick = () => {
    window.focus()
    router.push(`/applog/${event.id}`)
    n.close()
  }
}

export function useNotifications() {
  return { supported, permission, enabled, requestPermission, setEnabled, notifySrvlog, notifyNetlog, notifyApplog }
}
