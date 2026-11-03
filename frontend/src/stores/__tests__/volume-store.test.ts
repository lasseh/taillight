import { describe, it, expect, beforeEach } from 'vitest'
import { createApp } from 'vue'
import { setActivePinia, createPinia } from 'pinia'
import type { VolumeResponse } from '@/types/stats'
import { createVolumeStore, OTHER_GROUP } from '../volume-store'

beforeEach(() => {
  const app = createApp({})
  const pinia = createPinia()
  app.use(pinia)
  setActivePinia(pinia)
})

describe('volume store — capped by_host', () => {
  it('adds the other series after the named groups when the server folded groups', () => {
    const useStore = createVolumeStore('volume-test', () => Promise.resolve({ data: [] }), 'hosts')
    const store = useStore()
    store.buckets = [
      { time: '2026-06-05T10:00:00Z', total: 10, by_host: { web01: 4, db01: 3 }, other: 3 },
      { time: '2026-06-05T11:00:00Z', total: 2, by_host: { web01: 2 }, other: 0 },
    ]
    expect(store.groups).toEqual(['db01', 'web01', OTHER_GROUP])
    expect(store.chartData[0]).toMatchObject({ total: 10, web01: 4, db01: 3, [OTHER_GROUP]: 3 })
    expect(store.chartData[1]).toMatchObject({ total: 2, web01: 2, db01: 0, [OTHER_GROUP]: 0 })
  })

  it('has no other series when nothing was folded', async () => {
    const res: VolumeResponse = {
      data: [{ time: '2026-06-05T10:00:00Z', total: 1, by_host: { web01: 1 }, other: 0 }],
    }
    const useStore = createVolumeStore('volume-test-2', () => Promise.resolve(res), 'hosts')
    const store = useStore()
    await store.fetchVolume()
    expect(store.groups).toEqual(['web01'])
  })
})
