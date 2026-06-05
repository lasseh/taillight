// @vitest-environment jsdom
import { describe, it, expect, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { ref, defineComponent, h, inject, type Ref } from 'vue'
import { useCollapseOnEscape } from '../useCollapseOnEscape'
import { useDeviceLogScroll } from '../useDeviceLogScroll'

function fakeEl(metrics: { scrollHeight: number; scrollTop: number; clientHeight: number }) {
  return { scrollTo: vi.fn(), ...metrics } as unknown as HTMLElement
}

describe('useCollapseOnEscape', () => {
  it('provides collapseSignal, increments on Escape only, and detaches on unmount', async () => {
    let signal: Ref<number> | undefined
    const Child = defineComponent({
      setup() {
        signal = inject<Ref<number>>('collapseSignal')
        return () => h('div')
      },
    })
    const Host = defineComponent({
      setup() {
        useCollapseOnEscape()
        return () => h(Child)
      },
    })
    const wrapper = mount(Host)
    expect(signal?.value).toBe(0)

    document.dispatchEvent(new KeyboardEvent('keydown', { code: 'Escape' }))
    expect(signal?.value).toBe(1)

    // Non-Escape keys are ignored.
    document.dispatchEvent(new KeyboardEvent('keydown', { code: 'Enter' }))
    expect(signal?.value).toBe(1)

    // Listener is removed on unmount.
    wrapper.unmount()
    document.dispatchEvent(new KeyboardEvent('keydown', { code: 'Escape' }))
    expect(signal?.value).toBe(1)
  })
})

describe('useDeviceLogScroll', () => {
  type Api = ReturnType<typeof useDeviceLogScroll>

  function mountScroll(el: Ref<HTMLElement | null>, logs: Ref<unknown[]>, tab: Ref<string>) {
    let api: Api | undefined
    const Host = defineComponent({
      setup() {
        api = useDeviceLogScroll(el, logs, tab)
        return () => h('div')
      },
    })
    const wrapper = mount(Host)
    return { wrapper, api: api! }
  }

  it('scrollToBottom scrolls the bound element and re-pins', () => {
    const el = ref<HTMLElement | null>(null)
    const { api } = mountScroll(el, ref([]), ref('critical'))

    api.isPinned.value = false
    el.value = fakeEl({ scrollHeight: 1000, scrollTop: 0, clientHeight: 300 })
    api.scrollToBottom()

    expect(el.value!.scrollTo).toHaveBeenCalledWith({ top: 1000, behavior: 'instant' })
    expect(api.isPinned.value).toBe(true)
  })

  it('onLogScroll pins only when within 30px of the bottom', () => {
    const el = ref<HTMLElement | null>(null)
    const { api } = mountScroll(el, ref([]), ref('critical'))

    // 1000 - 0 - 300 = 700 from bottom -> not pinned.
    el.value = fakeEl({ scrollHeight: 1000, scrollTop: 0, clientHeight: 300 })
    api.onLogScroll()
    expect(api.isPinned.value).toBe(false)

    // 1000 - 690 - 300 = 10 from bottom -> pinned.
    el.value = fakeEl({ scrollHeight: 1000, scrollTop: 690, clientHeight: 300 })
    api.onLogScroll()
    expect(api.isPinned.value).toBe(true)
  })

  it('re-pins and scrolls to bottom on tab switch', async () => {
    const el = ref<HTMLElement | null>(fakeEl({ scrollHeight: 500, scrollTop: 0, clientHeight: 100 }))
    const tab = ref('critical')
    const { api } = mountScroll(el, ref([]), tab)
    api.isPinned.value = false

    tab.value = 'recent'
    await flushPromises()

    expect(api.isPinned.value).toBe(true)
    expect(el.value!.scrollTo).toHaveBeenCalledWith({ top: 500, behavior: 'instant' })
  })
})
