// @vitest-environment jsdom
//
// The meta search golden lists the endpoints that take the search parameter,
// under its name and max length; api/internal/handler proves it against the
// real handlers. This checks the API client sends it to each of them, and
// leaves the request as it was without a search. jsdom is required because
// config.ts dereferences `window` at import time.
import { describe, it, expect, vi, afterEach } from 'vitest'
import { api } from '@/lib/api'

import metaSearchGolden from '../../../../api/internal/handler/testdata/golden/meta_search_params.json'

const clients: Record<string, (search?: string) => Promise<unknown>> = {
  '/api/v1/srvlog/meta/hosts': (s) => api.getHosts(s),
  '/api/v1/srvlog/meta/programs': (s) => api.getPrograms(s),
  '/api/v1/srvlog/meta/tags': (s) => api.getTags(s),
  '/api/v1/netlog/meta/hosts': (s) => api.getNetlogHosts(s),
  '/api/v1/netlog/meta/programs': (s) => api.getNetlogPrograms(s),
  '/api/v1/netlog/meta/tags': (s) => api.getNetlogTags(s),
  '/api/v1/applog/meta/services': (s) => api.getAppLogServices(s),
  '/api/v1/applog/meta/components': (s) => api.getAppLogComponents(s),
  '/api/v1/applog/meta/hosts': (s) => api.getAppLogHosts(s),
  '/api/v1/analysis/hosts': (s) => api.listAnalysisHosts('netlog', s),
  '/api/v1/analysis/services': (s) => api.listAnalysisServices(s),
}

let fetchMock: ReturnType<typeof vi.fn>

async function requestedURL(call: () => Promise<unknown>): Promise<URL> {
  fetchMock = vi.fn(() =>
    Promise.resolve(
      new Response(JSON.stringify({ data: [] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    ),
  )
  vi.stubGlobal('fetch', fetchMock)
  await call()
  return new URL(String(fetchMock.mock.lastCall![0]), 'http://localhost')
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('meta search golden', () => {
  it('has a client call for every endpoint it lists', () => {
    expect(Object.keys(clients).sort()).toEqual([...metaSearchGolden.endpoints].sort())
  })

  for (const endpoint of metaSearchGolden.endpoints) {
    it(`${endpoint} sends ${metaSearchGolden.param} only when given`, async () => {
      const call = clients[endpoint]!
      const search = 'core 1/&x'.padEnd(metaSearchGolden.max_length, 'x')

      const withSearch = await requestedURL(() => call(search))
      expect(withSearch.pathname).toBe(endpoint)
      expect(withSearch.searchParams.get(metaSearchGolden.param)).toBe(search)

      const without = await requestedURL(() => call())
      expect(without.pathname).toBe(endpoint)
      expect(without.searchParams.has(metaSearchGolden.param)).toBe(false)
    })
  }
})
