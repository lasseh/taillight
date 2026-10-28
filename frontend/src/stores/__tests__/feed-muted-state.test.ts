// @vitest-environment jsdom
import { describe, it, expect, vi, beforeEach } from 'vitest'

// The netlog and srvlog stores keep "show muted" in localStorage. jsdom here
// has no localStorage, so install an in-memory stub that survives the module
// resets below, which stand in for a page reload.
const mem = vi.hoisted(() => {
  const mem: Record<string, string> = {}
  vi.stubGlobal('localStorage', {
    getItem: (k: string) => mem[k] ?? null,
    setItem: (k: string, v: string) => {
      mem[k] = v
    },
    removeItem: (k: string) => {
      delete mem[k]
    },
  })
  return mem
})

// A route that is neither feed keeps the filter watch from refetching.
vi.mock('vue-router', () => ({
  useRoute: () => ({ name: 'home', query: {} }),
  useRouter: () => ({ replace: vi.fn(() => Promise.resolve()) }),
}))

vi.mock('@/lib/api', () => ({
  api: {
    getNetlogs: vi.fn(() => Promise.resolve({ data: [], has_more: false })),
    getSrvlogs: vi.fn(() => Promise.resolve({ data: [], has_more: false })),
  },
}))

const feeds = [
  {
    feed: 'netlog',
    load: async () => (await import('@/stores/netlog-events')).useNetlogEventStore(),
  },
  {
    feed: 'srvlog',
    load: async () => (await import('@/stores/srvlog-events')).useSrvlogEventStore(),
  },
]

// Fresh module graph and pinia, as after a page reload.
async function reload<T>(load: () => Promise<T>): Promise<T> {
  vi.resetModules()
  const { createApp } = await import('vue')
  const { createPinia, setActivePinia } = await import('pinia')
  const pinia = createPinia()
  createApp({}).use(pinia)
  setActivePinia(pinia)
  return load()
}

describe.each(feeds)('$feed show-muted state', ({ feed, load }) => {
  beforeEach(() => {
    for (const k of Object.keys(mem)) delete mem[k]
  })

  it('defaults to hiding muted events', async () => {
    const store = await reload(load)
    expect(store.showMuted).toBe(false)
  })

  it('persists the choice across a reload', async () => {
    const store = await reload(load)
    store.toggleMuted()
    const { nextTick } = await import('vue')
    await nextTick()
    expect(mem[`taillight.${feed}.muted.visible`]).toBe('true')

    const reloaded = await reload(load)
    expect(reloaded.showMuted).toBe(true)
  })
})
