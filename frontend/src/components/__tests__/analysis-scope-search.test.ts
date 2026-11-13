// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import AnalysisReports from '../AnalysisReports.vue'
import { api } from '@/lib/api'
import { useAuthStore } from '@/stores/auth'
import type { AuthUser } from '@/types/auth'
import type { AnalysisFeed, AnalysisHostListResponse } from '@/types/analysis'

// The default lists stand in for capped ones that lack zz-core-1; only a
// search finds it, on either syslog feed.
function hostsFor(_feed: AnalysisFeed, search?: string): Promise<AnalysisHostListResponse> {
  const names = search ? ['zz-core-1', 'zz-core-10'] : ['a-000001', 'a-000002']
  return Promise.resolve({
    data: names.filter((n) => !search || n.includes(search)).map((hostname) => ({ hostname })),
  })
}

let listHosts: ReturnType<typeof vi.spyOn>

beforeEach(() => {
  vi.useFakeTimers()
  setActivePinia(createPinia())
  useAuthStore().user = { username: 'u', is_admin: true } as AuthUser
  vi.spyOn(api, 'listAnalysisReports').mockResolvedValue({ data: [] })
  listHosts = vi.spyOn(api, 'listAnalysisHosts').mockImplementation(hostsFor)
})

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})

async function openPicker(): Promise<VueWrapper> {
  const wrapper = mount(AnalysisReports, { global: { stubs: { RouterLink: true } } })
  await flushPromises()
  const open = wrapper.findAll('button').find((b) => b.text().includes('generate report'))!
  await open.trigger('click')
  return wrapper
}

const hostInput = (w: VueWrapper) => w.get('input[type="text"]')

function chips(w: VueWrapper): string[] {
  return w.findAll('span.inline-flex').map((c) => c.text().replace('×', '').trim())
}

describe('analysis scope picker server search', () => {
  it('suggests and picks a host the loaded list lacks', async () => {
    const wrapper = await openPicker()

    await hostInput(wrapper).setValue('zz-core-1')
    vi.advanceTimersByTime(300)
    await flushPromises()
    expect(listHosts).toHaveBeenLastCalledWith('netlog', 'zz-core-1')
    expect(wrapper.text()).toContain('zz-core-10')

    await hostInput(wrapper).trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(chips(wrapper)).toEqual(['zz-core-1'])
  })

  it('picks the server match on Enter before the debounce fires', async () => {
    const wrapper = await openPicker()

    await hostInput(wrapper).setValue('zz-core-1')
    await hostInput(wrapper).trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(chips(wrapper)).toEqual(['zz-core-1'])
  })

  it('keeps a picked host the new feed knows when the feed changes', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const wrapper = await openPicker()
    await hostInput(wrapper).setValue('zz-core-1')
    await hostInput(wrapper).trigger('keydown', { key: 'Enter' })
    await flushPromises()

    const srvlog = wrapper.findAll('button').find((b) => b.text().includes('Srvlog'))!
    await srvlog.trigger('click')
    await flushPromises()

    expect(listHosts).toHaveBeenCalledWith('srvlog', 'zz-core-1')
    expect(confirm).not.toHaveBeenCalled()
    expect(chips(wrapper)).toEqual(['zz-core-1'])
  })

  it('still offers to drop a picked host the new feed does not know', async () => {
    listHosts.mockImplementation((feed: AnalysisFeed, search?: string) =>
      feed === 'srvlog' ? Promise.resolve({ data: [] }) : hostsFor(feed, search),
    )
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const wrapper = await openPicker()
    await hostInput(wrapper).setValue('zz-core-1')
    await hostInput(wrapper).trigger('keydown', { key: 'Enter' })
    await flushPromises()

    const srvlog = wrapper.findAll('button').find((b) => b.text().includes('Srvlog'))!
    await srvlog.trigger('click')
    await flushPromises()

    expect(confirm).toHaveBeenCalledOnce()
    expect(chips(wrapper)).toEqual([])
  })

  it('keeps a picked host whose lookup on the new feed fails', async () => {
    listHosts.mockImplementation((feed: AnalysisFeed, search?: string) =>
      feed === 'srvlog' && search
        ? Promise.reject(new Error('429 too_many_requests'))
        : hostsFor(feed, search),
    )
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const wrapper = await openPicker()
    await hostInput(wrapper).setValue('zz-core-1')
    await hostInput(wrapper).trigger('keydown', { key: 'Enter' })
    await flushPromises()

    const srvlog = wrapper.findAll('button').find((b) => b.text().includes('Srvlog'))!
    await srvlog.trigger('click')
    await flushPromises()

    expect(confirm).not.toHaveBeenCalled()
    expect(chips(wrapper)).toEqual(['zz-core-1'])
  })
})
