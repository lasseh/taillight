// @vitest-environment jsdom
//
// A failed features fetch used to latch analysis off for the life of the page:
// the router is built once from these flags, so a one-second API restart during
// a deploy presented as "the analysis feature is not enabled on this instance".
// jsdom is required because lib/config dereferences `window` at import time.
import { describe, it, expect, vi, afterEach } from 'vitest'

// features.ts keeps module-level state, so each case needs a fresh instance.
async function freshModule() {
  vi.resetModules()
  return import('@/lib/features')
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

const serverFlags = {
  netlog: true,
  srvlog: true,
  applog: true,
  analysis: true,
  oidc: false,
}

afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('loadFeatures', () => {
  it('uses the server flags and reports them as loaded', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(jsonResponse(serverFlags))),
    )
    const { loadFeatures, features, featuresLoaded } = await freshModule()

    await loadFeatures()

    expect(features().analysis).toBe(true)
    expect(featuresLoaded()).toBe(true)
  })

  it('retries a transient failure and keeps the flags that eventually arrive', async () => {
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    // Fails once — an API restarting mid-deploy — then succeeds.
    const fetchMock = vi
      .fn()
      .mockRejectedValueOnce(new TypeError('Failed to fetch'))
      .mockResolvedValueOnce(jsonResponse(serverFlags))
    vi.stubGlobal('fetch', fetchMock)
    const { loadFeatures, features, featuresLoaded } = await freshModule()

    await loadFeatures()

    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(features().analysis).toBe(true)
    expect(featuresLoaded()).toBe(true)
  })

  it('retries a non-ok status too, not just a thrown error', async () => {
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(new Response(null, { status: 502 }))
      .mockResolvedValueOnce(jsonResponse(serverFlags))
    vi.stubGlobal('fetch', fetchMock)
    const { loadFeatures, features } = await freshModule()

    await loadFeatures()

    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(features().analysis).toBe(true)
  })

  it('gives up after the retries and reports the flags as not loaded', async () => {
    vi.spyOn(console, 'warn').mockImplementation(() => {})
    const fetchMock = vi.fn(() => Promise.reject(new TypeError('Failed to fetch')))
    vi.stubGlobal('fetch', fetchMock)
    const { loadFeatures, features, featuresLoaded } = await freshModule()

    await loadFeatures()

    // One initial attempt plus one per configured retry delay.
    expect(fetchMock).toHaveBeenCalledTimes(3)
    // Defaults stand, but featuresLoaded() marks them as a guess so the gated
    // view says "configuration unavailable" instead of "not enabled".
    expect(features().analysis).toBe(false)
    expect(featuresLoaded()).toBe(false)
  })

  it('reports not-loaded before any fetch has happened', async () => {
    const { featuresLoaded, features } = await freshModule()

    expect(featuresLoaded()).toBe(false)
    expect(features().netlog).toBe(true)
  })
})
