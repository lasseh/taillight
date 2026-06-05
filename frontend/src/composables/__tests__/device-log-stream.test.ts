// @vitest-environment jsdom
//
// Characterization tests for the three device-log composables. The blast radius
// of the createDeviceLogStream factory extraction (#16) is the per-feed
// parameterization — event type, API method, stream path, stream name, and the
// host/hostname query-param key — so these assert exactly those, plus one
// happy-path for the shared dedup/cap logic.
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ref, type Ref } from 'vue'
import { mount, flushPromises } from '@vue/test-utils'

// Capture every createEventStream(path, name) call and its subscribe callback.
interface FakeStream {
  path: string
  name: string
  connected: Ref<boolean>
  cb: ((e: unknown) => void) | null
  subscribe: ReturnType<typeof vi.fn>
  start: ReturnType<typeof vi.fn>
  stop: ReturnType<typeof vi.fn>
}
const streams: FakeStream[] = []
vi.mock('@/composables/useEventStream', () => ({
  createEventStream: vi.fn((path: string, name: string) => {
    const s: FakeStream = {
      path,
      name,
      connected: ref(false),
      cb: null,
      subscribe: vi.fn((cb: (e: unknown) => void) => {
        s.cb = cb
        return () => {}
      }),
      start: vi.fn(),
      stop: vi.fn(),
    }
    streams.push(s)
    return s
  }),
}))

// Mock the api methods and capture the URLSearchParams each receives.
const getSrvlogs = vi.fn(() => Promise.resolve({ data: [] as unknown[] }))
const getNetlogs = vi.fn(() => Promise.resolve({ data: [] as unknown[] }))
const getAppLogs = vi.fn(() => Promise.resolve({ data: [] as unknown[] }))
vi.mock('@/lib/api', () => ({
  api: {
    getSrvlogs: (...a: unknown[]) => getSrvlogs(...(a as [])),
    getNetlogs: (...a: unknown[]) => getNetlogs(...(a as [])),
    getAppLogs: (...a: unknown[]) => getAppLogs(...(a as [])),
  },
}))

import { useDeviceLogs } from '../useDeviceLogs'
import { useNetlogDeviceLogs } from '../useNetlogDeviceLogs'
import { useAppLogDeviceLogs } from '../useAppLogDeviceLogs'

// ES2020 lib (per tsconfig) lacks Array.prototype.at, so index the last element.
function last<T>(a: T[]): T {
  return a[a.length - 1] as T
}
function lastParams(fn: ReturnType<typeof vi.fn>): URLSearchParams {
  return last(fn.mock.calls)[0] as URLSearchParams
}

function mountComposable(useFn: (h: Ref<string>) => unknown, host: string) {
  const hostname = ref(host)
  const wrapper = mount({
    setup: () => useFn(hostname) as Record<string, unknown>,
    template: '<div />',
  })
  return { wrapper, hostname }
}

beforeEach(() => {
  streams.length = 0
  getSrvlogs.mockClear()
  getNetlogs.mockClear()
  getAppLogs.mockClear()
})

describe('device-log composables — per-feed parameterization', () => {
  it('srvlog: streams /srvlog/stream?hostname= and fetches with hostname param', async () => {
    mountComposable(useDeviceLogs, 'router1')
    await flushPromises()
    expect(last(streams).path).toBe('/api/v1/srvlog/stream?hostname=router1')
    expect(last(streams).name).toBe('srvlog')
    expect(lastParams(getSrvlogs).get('hostname')).toBe('router1')
    expect(lastParams(getSrvlogs).get('limit')).toBe('100')
  })

  it('netlog: streams /netlog/stream?hostname= and fetches with hostname param', async () => {
    mountComposable(useNetlogDeviceLogs, 'switch2')
    await flushPromises()
    expect(last(streams).path).toBe('/api/v1/netlog/stream?hostname=switch2')
    expect(last(streams).name).toBe('netlog')
    expect(lastParams(getNetlogs).get('hostname')).toBe('switch2')
    expect(lastParams(getNetlogs).get('limit')).toBe('100')
  })

  it('applog: streams /applog/stream?host= and fetches with the HOST (not hostname) param', async () => {
    mountComposable(useAppLogDeviceLogs, 'app3')
    await flushPromises()
    expect(last(streams).path).toBe('/api/v1/applog/stream?host=app3')
    expect(last(streams).name).toBe('applog')
    expect(lastParams(getAppLogs).get('host')).toBe('app3')
    expect(lastParams(getAppLogs).get('hostname')).toBeNull()
    expect(lastParams(getAppLogs).get('limit')).toBe('100')
  })

  it('encodes the hostname in both the stream path and the fetch param', async () => {
    mountComposable(useDeviceLogs, 'a b/c')
    await flushPromises()
    expect(last(streams).path).toBe('/api/v1/srvlog/stream?hostname=a%20b%2Fc')
    expect(lastParams(getSrvlogs).get('hostname')).toBe('a b/c')
  })
})

describe('device-log composables — shared live-tail logic', () => {
  it('dedupes by id, prepends newest, and caps the buffer at 200', async () => {
    const { wrapper } = mountComposable(useDeviceLogs, 'r1')
    await flushPromises()
    const stream = last(streams)
    const events = () => (wrapper.vm as unknown as { events: { id: number }[] }).events

    stream.cb!({ id: 1, message: 'a' })
    stream.cb!({ id: 1, message: 'dup' }) // same id → ignored
    stream.cb!({ id: 2, message: 'b' })
    expect(events().map((e) => e.id)).toEqual([2, 1]) // newest unshifted to front

    for (let i = 100; i < 320; i++) stream.cb!({ id: i, message: String(i) })
    expect(events().length).toBe(200) // capped
  })

  it('rewires the stream + fetch when the hostname changes', async () => {
    const { hostname } = mountComposable(useDeviceLogs, 'first')
    await flushPromises()
    const firstStream = last(streams)

    hostname.value = 'second'
    await flushPromises()
    expect(firstStream.stop).toHaveBeenCalled() // old stream torn down
    expect(last(streams).path).toBe('/api/v1/srvlog/stream?hostname=second')
    expect(lastParams(getSrvlogs).get('hostname')).toBe('second')
  })
})
