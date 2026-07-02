// @vitest-environment jsdom
//
// Tests for handleResponse's error/204 branches (issue #36), exercised through
// the public api methods with a stubbed global fetch. jsdom is required
// because config.ts dereferences `window` at import time.
import { describe, it, expect, vi, afterEach } from 'vitest'
import { api, ApiError } from '@/lib/api'

function stubFetch(res: Response) {
  vi.stubGlobal(
    'fetch',
    vi.fn(() => Promise.resolve(res)),
  )
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('handleResponse', () => {
  it('parses the JSON body on 200', async () => {
    stubFetch(
      new Response(JSON.stringify({ data: { id: 7 } }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' },
      }),
    )
    await expect(api.getSrvlog(7)).resolves.toEqual({ data: { id: 7 } })
  })

  it('resolves undefined on 204 — the contract for every Promise<void> mutation', async () => {
    stubFetch(new Response(null, { status: 204 }))
    await expect(api.deleteChannel(1)).resolves.toBeUndefined()
  })

  it('throws ApiError with code/message from the error envelope', async () => {
    stubFetch(
      new Response(JSON.stringify({ error: { code: 'not_found', message: 'no such event' } }), {
        status: 500,
        statusText: 'Internal Server Error',
      }),
    )
    const err = await api.getSrvlog(1).catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(500)
    expect((err as ApiError).code).toBe('not_found')
    expect((err as ApiError).message).toBe('no such event')
  })

  it('falls back to unknown/statusText on a non-JSON error body', async () => {
    stubFetch(new Response('<html>gateway error</html>', { status: 502, statusText: 'Bad Gateway' }))
    const err = await api.getSrvlog(1).catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).status).toBe(502)
    expect((err as ApiError).code).toBe('unknown')
    expect((err as ApiError).message).toBe('Bad Gateway')
  })

  it('falls back per-field when the error envelope is partial', async () => {
    stubFetch(
      new Response(JSON.stringify({ error: { code: 'rate_limited' } }), {
        status: 429,
        statusText: 'Too Many Requests',
      }),
    )
    const err = await api.getSrvlog(1).catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect((err as ApiError).code).toBe('rate_limited')
    expect((err as ApiError).message).toBe('Too Many Requests')
  })
})
