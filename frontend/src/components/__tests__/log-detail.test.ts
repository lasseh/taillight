// @vitest-environment jsdom
import { describe, it, expect, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { defineComponent, h } from 'vue'
import LogDetail from '../LogDetail.vue'
import { useAuthStore } from '@/stores/auth'
import type { AuthUser } from '@/types/auth'
import type { NetlogEvent } from '@/types/netlog'

const RouterLink = defineComponent({
  setup:
    (_, { slots }) =>
    () =>
      h('a', slots.default?.()),
})

function ev(overrides: Partial<NetlogEvent> = {}): NetlogEvent {
  return {
    id: 1,
    received_at: '2026-10-28T10:00:00Z',
    hostname: 'router1',
    programname: 'rpd',
    severity: 4,
    severity_label: 'warning',
    message: 'bundle ae30 flapped',
    ...overrides,
  } as NetlogEvent
}

function mountDetail(event: NetlogEvent, isAdmin: boolean) {
  useAuthStore().user = { username: 'u', is_admin: isAdmin } as AuthUser
  return mount(LogDetail, {
    props: {
      event,
      feed: 'netlog',
      filterStore: { filters: { programname: '', facility: '', severity_max: '' } },
      detailRoute: 'netlog-detail',
      deviceRoute: 'netlog-device',
    },
    global: { stubs: { RouterLink } },
  })
}

// The admin icons sit absolutely in the top-right corner; every row they can
// cover reserves room for them.
function paddedRows(wrapper: ReturnType<typeof mountDetail>): string[] {
  return wrapper.findAll('[data-copytext]').flatMap((row) => {
    const text = row.attributes('data-copytext')!
    return row.classes().includes('pr-16') ? [text.slice(0, text.indexOf(':'))] : []
  })
}

describe('LogDetail', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('keeps the muted line clear of the admin icons', () => {
    expect(paddedRows(mountDetail(ev({ muted_by: 5 }), true))).toEqual(['muted', 'time'])
  })

  it('pads only the time row on an unmuted event', () => {
    expect(paddedRows(mountDetail(ev(), true))).toEqual(['time'])
  })

  it('pads nothing for a non-admin, who sees only the one permalink icon', () => {
    expect(paddedRows(mountDetail(ev({ muted_by: 5 }), false))).toEqual([])
  })
})
