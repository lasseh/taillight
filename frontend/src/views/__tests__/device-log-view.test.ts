// @vitest-environment jsdom
import { describe, it, expect, vi } from 'vitest'

// useTheme.ts (pulled in transitively via DeviceActivityChart) reads localStorage
// at module load, before any beforeEach can run, and jsdom here has no
// localStorage — install an in-memory stub before the imports below are evaluated.
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

import { mount, flushPromises } from '@vue/test-utils'
import { ref, defineComponent, h, markRaw } from 'vue'
import DeviceLogView from '../DeviceLogView.vue'
import type { SrvlogEvent } from '@/types/srvlog'
import type { DeviceSummaryResponse } from '@/types/device'

// vue-router is stubbed: the view only needs useRouter().back/replace,
// useRoute().query, and a RouterLink that renders its slot.
vi.mock('vue-router', () => ({
  useRouter: () => ({ replace: vi.fn(), back: vi.fn() }),
  useRoute: () => ({ query: {} }),
  RouterLink: defineComponent({
    name: 'RouterLink',
    setup: (_, { slots }) => () => h('a', slots.default?.()),
  }),
}))

// jsdom doesn't implement Element.scrollTo; the view calls it on tab switch.
window.HTMLElement.prototype.scrollTo = vi.fn()

// markRaw so VTU's reactive props don't proxy the component — passing a
// component through reactive state triggers a Vue "made reactive" warning.
// Production callers bind :row statically, so this only bites under test.
const RowStub = markRaw(
  defineComponent({
    name: 'RowStub',
    props: { event: { type: Object, required: true } },
    setup: (p) => () => h('div', { class: 'row-stub' }, String((p.event as SrvlogEvent).id)),
  }),
)

function ev(id: number): SrvlogEvent {
  return { id, programname: 'prog', severity: 6, received_at: '2026-06-05T10:00:00Z' } as unknown as SrvlogEvent
}

function makeSummary(): DeviceSummaryResponse {
  return {
    data: {
      hostname: 'host-1',
      fromhost_ip: '10.0.0.1',
      last_seen_at: '2026-06-05T10:00:00Z',
      total_count: 3,
      critical_count: 1,
      severity_breakdown: [{ severity: 2, label: 'crit', count: 1, pct: 100 }],
      top_messages: [],
      critical_logs: [ev(1)],
      activity: [],
    },
  }
}

function mountView(events = ref<SrvlogEvent[]>([ev(10), ev(11)])) {
  const fetchSummary = vi.fn(async () => makeSummary())
  const wrapper = mount(DeviceLogView, {
    props: {
      hostname: 'host-1',
      useLogs: () => ({ events }),
      fetchSummary,
      row: RowStub,
      listRoute: 'srvlog',
      listLabel: 'go to srvlog',
      detailRoute: 'srvlog-detail',
      highlightPrefix: 'srvlog',
    },
  })
  return { wrapper, fetchSummary }
}

describe('DeviceLogView (smoke)', () => {
  it('loads the summary, renders tabs, and shows critical logs by default', async () => {
    const { wrapper, fetchSummary } = mountView()
    expect(wrapper.text()).toContain('loading')

    await flushPromises()

    expect(fetchSummary).toHaveBeenCalledWith('host-1')
    const tabs = wrapper.findAll('button').map((b) => b.text())
    expect(tabs).toContain('Critical Logs')
    expect(tabs).toContain('Recent Logs')
    // Critical tab default: the one critical_logs entry (id 1) renders.
    expect(wrapper.findAll('.row-stub').map((r) => r.text())).toEqual(['1'])
    wrapper.unmount()
  })

  it('updates summary stats live when a new event is unshifted into the stream', async () => {
    // Seed newest id is 11, so fetchData baselines the cursor there; a newer
    // critical event must tick the summary up. Before the reactivity fix the
    // in-place unshift never fired the watcher and the stats stayed frozen.
    const events = ref<SrvlogEvent[]>([ev(11)])
    const { wrapper } = mountView(events)
    await flushPromises()
    // Critical tab default renders the one seeded critical log (id 1).
    expect(wrapper.findAll('.row-stub').map((r) => r.text())).toEqual(['1'])

    const crit = { id: 12, programname: 'prog', severity: 2, received_at: '2026-06-05T10:01:00Z' } as unknown as SrvlogEvent
    events.value.unshift(crit) // in-place, exactly like createDeviceLogStream
    await flushPromises()

    // critical_logs grew (newest-first -> chronological reverse -> [1, 12]).
    expect(wrapper.findAll('.row-stub').map((r) => r.text())).toEqual(['1', '12'])
    wrapper.unmount()
  })

  it('switches to the recent tab and renders the live event stream', async () => {
    const { wrapper } = mountView()
    await flushPromises()

    const recent = wrapper.findAll('button').find((b) => b.text() === 'Recent Logs')!
    await recent.trigger('click')
    await flushPromises()

    // Recent tab shows the two live events (chronological: 10 then 11).
    expect(wrapper.findAll('.row-stub').map((r) => r.text())).toEqual(['11', '10'])
    wrapper.unmount()
  })
})
