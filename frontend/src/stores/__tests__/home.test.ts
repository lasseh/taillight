// @vitest-environment jsdom
// The store's import chain reaches lib/config.ts, which reads window.__CONFIG__
// at module load, so this needs a DOM global even though the shaping logic doesn't.
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { createApp } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import type { SrvlogEvent } from '@/types/srvlog'
import type { NetlogEvent } from '@/types/netlog'
import type { SrvlogSummary } from '@/types/stats'
import { useHomeStore } from '../home'

function srv(id: number, received_at: string): SrvlogEvent {
  return { id, received_at } as unknown as SrvlogEvent
}
function net(id: number, received_at: string): NetlogEvent {
  return { id, received_at } as unknown as NetlogEvent
}

function summary(partial: Partial<SrvlogSummary>): SrvlogSummary {
  return {
    total: 0,
    trend: 0,
    errors: 0,
    warnings: 0,
    severity_breakdown: [],
    top_hosts: [],
    ...partial,
  }
}

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

describe('home store — combinedRecentEvents (cross-feed shaping)', () => {
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

describe('home store — combined syslog getters (cross-feed shaping)', () => {
  it('syslogTotal sums srvlog + netlog totals, treating a missing summary as 0', () => {
    const store = useHomeStore()
    expect(store.syslogTotal).toBe(0)

    store.srvlogSummary = summary({ total: 100 })
    expect(store.syslogTotal).toBe(100)

    store.netlogSummary = summary({ total: 50 })
    expect(store.syslogTotal).toBe(150)
  })

  it('syslogTrend is 0 when both summaries are missing', () => {
    const store = useHomeStore()
    expect(store.syslogTrend).toBe(0)
  })

  it('syslogTrend reconstructs each feed previous-period total and recombines', () => {
    const store = useHomeStore()
    // srvlog: 110 now, +10% → prev 100. netlog: 220 now, +10% → prev 200.
    // Combined: 330 vs 300 → +10%.
    store.srvlogSummary = summary({ total: 110, trend: 10 })
    store.netlogSummary = summary({ total: 220, trend: 10 })
    expect(store.syslogTrend).toBeCloseTo(10, 6)

    // srvlog: 150 now, +50% → prev 100. netlog: 100 now, -50% → prev 200.
    // Combined: 250 vs 300 → -16.667%.
    store.srvlogSummary = summary({ total: 150, trend: 50 })
    store.netlogSummary = summary({ total: 100, trend: -50 })
    expect(store.syslogTrend).toBeCloseTo(-16.667, 3)
  })

  it('syslogTrend uses the current total as previous when a feed trend is 0', () => {
    const store = useHomeStore()
    store.srvlogSummary = summary({ total: 100, trend: 0 })
    expect(store.syslogTrend).toBe(0)
  })

  it('syslogSeverityBreakdown merges same-severity counts, recomputes pct, sorts ascending', () => {
    const store = useHomeStore()
    store.srvlogSummary = summary({
      total: 30,
      severity_breakdown: [
        { severity: 3, label: 'err', count: 10, pct: 33.3 },
        { severity: 2, label: 'crit', count: 20, pct: 66.7 },
      ],
    })
    store.netlogSummary = summary({
      total: 10,
      severity_breakdown: [{ severity: 3, label: 'err', count: 10, pct: 100 }],
    })

    // Combined total 40: crit 20 (50%), err 10+10=20 (50%); input pct ignored.
    expect(store.syslogSeverityBreakdown).toEqual([
      { severity: 2, label: 'crit', count: 20, pct: 50 },
      { severity: 3, label: 'err', count: 20, pct: 50 },
    ])
  })

  it('syslogTopHosts merges by name, sums counts, tags feed (srvlog wins), sorts by count', () => {
    const store = useHomeStore()
    store.srvlogSummary = summary({
      total: 60,
      top_hosts: [
        { name: 'a', count: 30, pct: 50 },
        { name: 'b', count: 30, pct: 50 },
      ],
    })
    store.netlogSummary = summary({
      total: 40,
      top_hosts: [
        { name: 'b', count: 20, pct: 50 },
        { name: 'c', count: 20, pct: 50 },
      ],
    })

    // Combined total 100: b=30+20 (present in srvlog → srvlog), a=30, c=20.
    expect(store.syslogTopHosts).toEqual([
      { name: 'b', count: 50, pct: 50, feed: 'srvlog' },
      { name: 'a', count: 30, pct: 30, feed: 'srvlog' },
      { name: 'c', count: 20, pct: 20, feed: 'netlog' },
    ])
  })

  it('syslogHeatmap sums per-bucket counts across both feeds', () => {
    const store = useHomeStore()
    store.srvlogHeatmap = { '2026-06-05 10:00': 5, '2026-06-05 10:30': 2 }
    store.netlogHeatmap = { '2026-06-05 10:00': 3, '2026-06-05 11:00': 1 }

    expect(store.syslogHeatmap).toEqual({
      '2026-06-05 10:00': 8,
      '2026-06-05 10:30': 2,
      '2026-06-05 11:00': 1,
    })
  })
})
