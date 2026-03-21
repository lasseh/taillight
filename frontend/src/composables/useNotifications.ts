import { ref } from 'vue'
import router from '@/router'
import type { SyslogEvent } from '@/types/syslog'
import type { AppLogEvent } from '@/types/applog'
import { severityLabels } from '@/lib/constants'

/** Severity threshold — notify for events with severity <= this value. */
const NOTIFY_MAX_SEVERITY = 2 // emerg(0), alert(1), crit(2)

/** Applog levels that trigger browser push. */
const APPLOG_NOTIFY_LEVELS = new Set(['ERROR', 'FATAL'])

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

function notifySyslog(event: SyslogEvent) {
  if (!supported) return
  if (!enabled.value) return
  if (permission.value !== 'granted') return
  if (event.severity > NOTIFY_MAX_SEVERITY) return
  if (isTooOld(event.received_at)) return

  const level = severityLabels[event.severity] ?? 'unknown'
  const title = `[${level}] ${event.hostname}`
  const body = event.message.slice(0, 120)

  const n = new Notification(title, {
    body,
    tag: `syslog-${event.id}`,
  })
  n.onclick = () => {
    window.focus()
    router.push(`/syslog/${event.id}`)
    n.close()
  }
}

function notifyApplog(event: AppLogEvent) {
  if (!supported) return
  if (!enabled.value) return
  if (permission.value !== 'granted') return
  if (!APPLOG_NOTIFY_LEVELS.has(event.level)) return
  if (isTooOld(event.received_at)) return

  const title = `[${event.level}] ${event.service}`
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
  return { supported, permission, enabled, requestPermission, setEnabled, notifySyslog, notifyApplog }
}
