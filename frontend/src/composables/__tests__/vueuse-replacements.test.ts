// @vitest-environment jsdom
//
// Tests for the local replacements of the three @vueuse/core composables the
// app used (onClickOutside, useDebounceFn, useElementSize) — asserting the
// behavior the call sites rely on: dropdown close on outside pointerdown,
// trailing-edge debounce, and size tracking that resets when the ref clears.
import { describe, it, expect, vi, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { ref, nextTick, defineComponent, h, type Ref } from 'vue'
import { useClickOutside } from '../useClickOutside'
import { useDebounceFn } from '../useDebounceFn'
import { useElementSize } from '../useElementSize'

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

function pointerdown(el: Element) {
  el.dispatchEvent(new Event('pointerdown', { bubbles: true, composed: true }))
}

describe('useClickOutside', () => {
  it('fires on outside pointerdown only, and detaches on unmount', () => {
    const handler = vi.fn()
    const Host = defineComponent({
      setup() {
        const target = ref<HTMLElement | null>(null)
        useClickOutside(target, handler)
        return () =>
          h('div', [
            h('div', { ref: target, id: 'inside' }, [h('span', { id: 'inner' })]),
            h('div', { id: 'outside' }),
          ])
      },
    })
    const wrapper = mount(Host, { attachTo: document.body })

    pointerdown(wrapper.find('#inner').element)
    expect(handler).not.toHaveBeenCalled()

    pointerdown(wrapper.find('#outside').element)
    expect(handler).toHaveBeenCalledTimes(1)

    wrapper.unmount()
    pointerdown(document.body)
    expect(handler).toHaveBeenCalledTimes(1)
  })
})

describe('useDebounceFn', () => {
  it('collapses rapid calls into one trailing call with the last args', () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const debounced = useDebounceFn(fn, 300)

    debounced('a')
    debounced('b')
    vi.advanceTimersByTime(299)
    expect(fn).not.toHaveBeenCalled()

    vi.advanceTimersByTime(1)
    expect(fn).toHaveBeenCalledTimes(1)
    expect(fn).toHaveBeenCalledWith('b')
  })
})

describe('useElementSize', () => {
  class FakeResizeObserver {
    static last: FakeResizeObserver | undefined
    observed: Element[] = []
    cb: ResizeObserverCallback
    constructor(cb: ResizeObserverCallback) {
      this.cb = cb
      FakeResizeObserver.last = this
    }
    observe(el: Element) {
      this.observed.push(el)
    }
    disconnect() {
      this.observed = []
    }
    unobserve() {}
  }

  it('tracks the observed element and resets to zero when the ref clears', async () => {
    vi.stubGlobal('ResizeObserver', FakeResizeObserver)
    const target = ref<HTMLElement | null>(null)
    let size!: { width: Ref<number>; height: Ref<number> }
    const Host = defineComponent({
      setup() {
        size = useElementSize(target)
        return () => h('div')
      },
    })
    mount(Host)

    const el = document.createElement('div')
    target.value = el
    await nextTick()
    const ro = FakeResizeObserver.last!
    expect(ro.observed).toEqual([el])

    ro.cb([{ contentRect: { width: 120, height: 48 } } as ResizeObserverEntry], ro)
    expect(size.width.value).toBe(120)
    expect(size.height.value).toBe(48)

    target.value = null
    await nextTick()
    expect(ro.observed).toEqual([])
    expect(size.width.value).toBe(0)
    expect(size.height.value).toBe(0)
  })
})
