// @vitest-environment jsdom
import { describe, it, expect } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref, defineComponent, h, type Ref } from 'vue'
import { useDeviceSummaryLive } from '../useDeviceSummaryLive'

type Ev = { id: number }

// Mirror createDeviceLogStream: newest-first, mutated in place, capped at 200.
function pushLive(logs: Ref<Ev[]>, id: number) {
  logs.value.unshift({ id })
  if (logs.value.length > 200) logs.value.splice(200)
}

function mountLive(logs: Ref<Ev[]>) {
  const applied: number[] = []
  let api: ReturnType<typeof useDeviceSummaryLive<Ev>> | undefined
  const Host = defineComponent({
    setup() {
      api = useDeviceSummaryLive(logs, (e) => applied.push(e.id))
      return () => h('div')
    },
  })
  const wrapper = mount(Host)
  return { wrapper, applied, api: api! }
}

describe('useDeviceSummaryLive', () => {
  it('applies events added by in-place unshift (the bug this fixes)', async () => {
    const logs = ref<Ev[]>([])
    const { applied } = mountLive(logs)

    pushLive(logs, 1)
    await flushPromises()
    pushLive(logs, 2)
    await flushPromises()

    expect(applied).toEqual([1, 2])
  })

  it('does not re-apply events at or below the cursor', async () => {
    const logs = ref<Ev[]>([])
    const { applied } = mountLive(logs)

    pushLive(logs, 5)
    await flushPromises()
    expect(applied).toEqual([5])

    pushLive(logs, 6)
    await flushPromises()
    // Only 6 is new; 5 (now at the cursor) is not replayed.
    expect(applied).toEqual([5, 6])
  })

  it('baseline() skips the whole current buffer (no 200-event replay)', async () => {
    const logs = ref<Ev[]>([])
    for (let i = 1; i <= 50; i++) pushLive(logs, i)
    const { applied, api } = mountLive(logs)
    await flushPromises()

    // Caller re-baselines after a fresh poll: the buffer is already counted.
    api.baseline()
    applied.length = 0

    pushLive(logs, 51)
    await flushPromises()
    expect(applied).toEqual([51])
  })

  it('still fires once the buffer is capped at 200 (length stays constant)', async () => {
    const logs = ref<Ev[]>([])
    for (let i = 1; i <= 200; i++) pushLive(logs, i)
    const { applied, api } = mountLive(logs)
    await flushPromises()
    api.baseline()
    applied.length = 0
    expect(logs.value.length).toBe(200)

    pushLive(logs, 201) // unshift + splice -> length unchanged at 200
    await flushPromises()
    expect(logs.value.length).toBe(200)
    expect(applied).toEqual([201])
  })
})
