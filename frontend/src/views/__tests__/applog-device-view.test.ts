// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach } from 'vitest'

// useTheme.ts (pulled in transitively via DeviceActivityChart) reads localStorage
// at module load — install an in-memory stub before the imports below evaluate.
vi.hoisted(() => {
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
})

// The view calls useAppLogDeviceLogs (which would open an SSE stream); replace it
// with a fixed two-event live feed.
vi.mock('@/composables/useAppLogDeviceLogs', async () => {
  const { ref } = await import('vue')
  const e = (id: number) => ({
    id,
    level: 'INFO',
    service: 'svc',
    component: 'comp',
    received_at: '2026-06-05T10:00:00Z',
  })
  return { useAppLogDeviceLogs: () => ({ events: ref([e(10), e(11)]) }) }
})

import { mount, flushPromises } from '@vue/test-utils'
import AppLogDeviceView from '../AppLogDeviceView.vue'
import AppLogRow from '@/components/AppLogRow.vue'
import { api } from '@/lib/api'
import type { AppLogEvent } from '@/types/applog'
import type { AppLogDeviceSummaryResponse } from '@/types/device'

// jsdom doesn't implement Element.scrollTo; the view calls it on tab switch.
window.HTMLElement.prototype.scrollTo = vi.fn()

vi.mock('vue-router', () => ({
  useRouter: () => ({ replace: vi.fn(), back: vi.fn() }),
  useRoute: () => ({ query: {} }),
  RouterLink: { name: 'RouterLink', template: '<a><slot /></a>' },
}))

function ev(id: number): AppLogEvent {
  return {
    id,
    level: 'ERROR',
    service: 'svc',
    component: 'comp',
    received_at: '2026-06-05T10:00:00Z',
  } as unknown as AppLogEvent
}

function makeSummary(): AppLogDeviceSummaryResponse {
  return {
    data: {
      host: 'host-1',
      last_seen_at: '2026-06-05T10:00:00Z',
      total_count: 5,
      error_count: 1,
      level_breakdown: [{ level: 'ERROR', count: 1, pct: 100 }],
      top_messages: [],
      error_logs: [ev(1)],
      activity: [],
    },
  }
}

function mountView() {
  return mount(AppLogDeviceView, {
    props: { hostname: 'host-1' },
    global: { stubs: { AppLogRow: true } },
  })
}

describe('AppLogDeviceView (smoke)', () => {
  beforeEach(() => {
    vi.spyOn(api, 'getAppLogDeviceSummary').mockResolvedValue(makeSummary())
  })

  it('loads the summary, renders tabs, and shows error logs by default', async () => {
    const wrapper = mountView()
    expect(wrapper.text()).toContain('loading')

    await flushPromises()

    expect(api.getAppLogDeviceSummary).toHaveBeenCalledWith('host-1')
    const tabs = wrapper.findAll('button').map((b) => b.text())
    expect(tabs).toContain('Error Logs')
    expect(tabs).toContain('Recent Logs')
    // Error tab default: the one error_logs entry renders.
    expect(wrapper.findAllComponents(AppLogRow)).toHaveLength(1)
    wrapper.unmount()
  })

  it('switches to the recent tab and renders the live event stream', async () => {
    const wrapper = mountView()
    await flushPromises()

    const recent = wrapper.findAll('button').find((b) => b.text() === 'Recent Logs')!
    await recent.trigger('click')
    await flushPromises()

    // Recent tab shows the two live events from the mocked feed.
    expect(wrapper.findAllComponents(AppLogRow)).toHaveLength(2)
    wrapper.unmount()
  })
})
