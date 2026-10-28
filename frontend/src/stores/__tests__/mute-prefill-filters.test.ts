// The "mute similar" query lands on /notifications, and App.vue boots all
// three filter stores with initFromURL() whatever the route. The prefill keys
// must never reach those stores, or the feeds would open filtered.
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { createApp, reactive } from 'vue'

const holder = {
  route: reactive({ name: 'notifications', query: {} as Record<string, string> }),
}
vi.mock('vue-router', () => ({
  useRoute: () => holder.route,
  useRouter: () => ({ replace: vi.fn(() => Promise.resolve()) }),
}))

import { createPinia, setActivePinia } from 'pinia'
import { useNetlogFilterStore } from '../netlog-filters'
import { useSrvlogFilterStore } from '../srvlog-filters'
import { useAppLogFilterStore } from '../applog-filters'
import { muteSimilarQuery } from '@/lib/mute-rules'

function bootFilterStores(query: Record<string, string>) {
  holder.route = reactive({ name: 'notifications', query })
  const stores = [useNetlogFilterStore(), useSrvlogFilterStore(), useAppLogFilterStore()]
  for (const s of stores) s.initFromURL()
  return stores
}

const event = {
  hostname: '00a-leaf-d6e11-02',
  programname: 'kernel',
  message: 'bundle ae30: minimum links not met',
}

describe('mute similar prefill vs feed filters', () => {
  beforeEach(() => {
    const pinia = createPinia()
    createApp({}).use(pinia)
    setActivePinia(pinia)
  })

  it.each(['netlog', 'srvlog'] as const)('a %s prefill sets no feed filter', (feed) => {
    for (const s of bootFilterStores(muteSimilarQuery(feed, event))) {
      expect(s.activeFilters).toEqual({})
    }
  })

  it('an unprefixed key would set the feed filters (control)', () => {
    const [netlog, srvlog] = bootFilterStores({ tab: 'mutes', hostname: event.hostname })
    expect(netlog!.activeFilters).toEqual({ hostname: event.hostname })
    expect(srvlog!.activeFilters).toEqual({ hostname: event.hostname })
  })
})
