// @vitest-environment jsdom
//
// Browser push for the syslog feeds. A muted event (muted_by set) never
// raises a desktop notification, whether or not muted rows are shown.
import { beforeEach, describe, expect, it, vi } from 'vitest'

// useNotifications reads Notification and localStorage at module load, before
// any beforeEach can run, so both stubs are installed here.
const created = vi.hoisted(() => {
  const created: string[] = []
  class FakeNotification {
    static permission = 'granted'
    static requestPermission = () => Promise.resolve('granted')
    onclick: (() => void) | null = null
    constructor(title: string) {
      created.push(title)
    }
    close() {}
  }
  vi.stubGlobal('Notification', FakeNotification)

  const mem: Record<string, string> = {}
  vi.stubGlobal('localStorage', {
    getItem: (k: string) => mem[k] ?? null,
    setItem: (k: string, v: string) => {
      mem[k] = v
    },
    removeItem: (k: string) => {
      delete mem[k]
    },
    clear: () => {
      for (const k of Object.keys(mem)) delete mem[k]
    },
  })
  return created
})

vi.mock('@/router', () => ({ default: { push: vi.fn() } }))

import { createPinia, setActivePinia } from 'pinia'
import { useNotifications } from '../useNotifications'
import type { NetlogEvent } from '@/types/netlog'
import type { SrvlogEvent } from '@/types/srvlog'

// Netlog and srvlog events share one wire shape.
function critEvent(over: Partial<SrvlogEvent> = {}): SrvlogEvent & NetlogEvent {
  const now = new Date().toISOString()
  return {
    id: 1,
    received_at: now,
    reported_at: now,
    hostname: 'leaf-02',
    fromhost_ip: '192.0.2.1',
    programname: 'kernel',
    msgid: '',
    severity: 2,
    severity_label: 'crit',
    facility: 0,
    facility_label: 'kern',
    syslogtag: 'kernel:',
    message: 'lag_bundlestate_ifd_change: bundle ae30 minimum links not met',
    ...over,
  }
}

beforeEach(() => {
  setActivePinia(createPinia())
  created.length = 0
})

describe('browser notifications for muted events', () => {
  const { notifyNetlog, notifySrvlog } = useNotifications()

  it('notifies an unmuted severity-2 netlog event', () => {
    notifyNetlog(critEvent())
    expect(created).toHaveLength(1)
  })

  it('does not notify the same netlog event when it is muted', () => {
    notifyNetlog(critEvent({ muted_by: 5 }))
    expect(created).toHaveLength(0)
  })

  it('notifies an unmuted severity-2 srvlog event', () => {
    notifySrvlog(critEvent())
    expect(created).toHaveLength(1)
  })

  it('does not notify the same srvlog event when it is muted', () => {
    notifySrvlog(critEvent({ muted_by: 5 }))
    expect(created).toHaveLength(0)
  })
})
