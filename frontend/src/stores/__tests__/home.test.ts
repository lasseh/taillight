// @vitest-environment jsdom
// The store's import chain reaches lib/config.ts, which reads window.__CONFIG__
// at module load, so this needs a DOM global even though the shaping logic doesn't.
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { createApp } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import type { SrvlogEvent } from '@/types/srvlog'
import type { NetlogEvent } from '@/types/netlog'
import { useHomeStore } from '../home'

function srv(id: number, received_at: string): SrvlogEvent {
  return { id, received_at } as unknown as SrvlogEvent
}
function net(id: number, received_at: string): NetlogEvent {
  return { id, received_at } as unknown as NetlogEvent
}

describe('home store — combinedRecentEvents (cross-feed shaping)', () => {
  beforeEach(() => {
    // The store reads localStorage('home-range') at setup; jsdom's bare
    // localStorage global is unreliable, so provide a minimal in-memory stub.
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
    const app = createApp({})
    const pinia = createPinia()
    app.use(pinia)
    setActivePinia(pinia)
  })

  it('merges srvlog+netlog, tags _feed/_routeName, and sorts newest-first', () => {
    const store = useHomeStore()
    store.recentSrvlogEvents = [srv(1, '2026-06-05T10:00:00Z'), srv(2, '2026-06-05T08:00:00Z')]
    store.recentNetlogEvents = [net(3, '2026-06-05T09:00:00Z')]

    const out = store.combinedRecentEvents
    expect(out.map((e) => e.id)).toEqual([1, 3, 2]) // received_at descending

    const s1 = out.find((e) => e.id === 1)
    expect(s1?._feed).toBe('srvlog')
    expect(s1?._routeName).toBe('srvlog-detail')

    const n3 = out.find((e) => e.id === 3)
    expect(n3?._feed).toBe('netlog')
    expect(n3?._routeName).toBe('netlog-detail')
  })

  it('caps the combined list at 10 (newest kept)', () => {
    const store = useHomeStore()
    // 8 srvlog + 8 netlog = 16; minutes encode recency so the newest 10 survive.
    store.recentSrvlogEvents = Array.from({ length: 8 }, (_, i) =>
      srv(i, `2026-06-05T10:${String(i).padStart(2, '0')}:00Z`),
    )
    store.recentNetlogEvents = Array.from({ length: 8 }, (_, i) =>
      net(100 + i, `2026-06-05T11:${String(i).padStart(2, '0')}:00Z`),
    )

    const out = store.combinedRecentEvents
    expect(out.length).toBe(10)
    // The 8 netlog (11:xx) are all newer than every srvlog (10:xx), so the top
    // of the list is netlog and the oldest srvlog entries are dropped.
    expect(out.slice(0, 8).every((e) => e._feed === 'netlog')).toBe(true)
  })
})
