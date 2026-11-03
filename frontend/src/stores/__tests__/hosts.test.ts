// @vitest-environment jsdom
// The store's import chain reaches lib/config.ts, which reads window.__CONFIG__
// at module load, so this needs a DOM global.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { createApp, nextTick } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import { api } from '@/lib/api'
import type { HostEntry, HostsResponse } from '@/types/host'
import { useHostsStore } from '../hosts'

function page(n: number, total: number): HostsResponse {
  return {
    data: Array.from({ length: n }, (_, i) => ({ hostname: `h${i}` }) as HostEntry),
    total,
  }
}

let getHosts: ReturnType<typeof vi.spyOn>

function lastParams(): URLSearchParams {
  return getHosts.mock.lastCall![0] as URLSearchParams
}

beforeEach(() => {
  vi.useFakeTimers()
  const mem: Record<string, string> = {}
  vi.stubGlobal('localStorage', {
    getItem: (k: string) => mem[k] ?? null,
    setItem: (k: string, v: string) => {
      mem[k] = v
    },
  })
  const app = createApp({})
  const pinia = createPinia()
  app.use(pinia)
  setActivePinia(pinia)
  getHosts = vi.spyOn(api, 'getHostsSummary').mockResolvedValue(page(100, 250))
})

afterEach(() => {
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('hosts store — server-side search, filter, sort and paging', () => {
  it('sends the range, sort and first page, and keeps the server total', async () => {
    const store = useHostsStore()
    await store.fetchHosts()
    const p = lastParams()
    expect(p.get('range')).toBe('24h')
    expect(p.get('sort')).toBe('errors')
    expect(p.get('limit')).toBe('100')
    expect(p.get('offset')).toBe('0')
    expect(p.has('search')).toBe(false)
    expect(store.hosts).toHaveLength(100)
    expect(store.total).toBe(250)
  })

  it('sends filters and sort to the server and restarts at the first page', async () => {
    const store = useHostsStore()
    await store.fetchHosts()
    store.nextPage()
    await vi.runAllTimersAsync()
    expect(lastParams().get('offset')).toBe('100')

    store.statusFilter = 'critical'
    store.feedFilter = 'netlog'
    store.sortBy = 'hostname'
    await nextTick()
    await vi.runAllTimersAsync()
    const p = lastParams()
    expect(p.get('status')).toBe('critical')
    expect(p.get('feed')).toBe('netlog')
    expect(p.get('sort')).toBe('hostname')
    expect(p.get('offset')).toBe('0')
  })

  it('debounces the search into one request', async () => {
    const store = useHostsStore()
    getHosts.mockClear()
    store.search = 'w'
    await nextTick()
    store.search = 'web'
    await nextTick()
    await vi.runAllTimersAsync()
    expect(getHosts).toHaveBeenCalledTimes(1)
    expect(lastParams().get('search')).toBe('web')
  })

  it('stops paging at the total', async () => {
    getHosts.mockResolvedValue(page(50, 150))
    const store = useHostsStore()
    await store.fetchHosts()
    store.nextPage()
    await vi.runAllTimersAsync()
    expect(store.offset).toBe(100)
    getHosts.mockClear()
    store.nextPage()
    store.prevPage()
    await vi.runAllTimersAsync()
    expect(getHosts).toHaveBeenCalledTimes(1)
    expect(store.offset).toBe(0)
  })
})
