// @vitest-environment jsdom
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import FilterSelect from '../FilterSelect.vue'
import type { FilterOption } from '@/types/srvlog'

const opt = (v: string): FilterOption => ({ value: v, label: v })

// The loaded list stands in for a capped default list that lacks the host.
const loaded = ['a-000001', 'a-000002', 'core-10'].map(opt)

function mountSelect(fetchOptions?: (q: string) => Promise<FilterOption[]>) {
  const onUpdate = vi.fn()
  const wrapper = mount(FilterSelect, {
    props: {
      label: 'host',
      options: loaded,
      searchable: true,
      ...(fetchOptions ? { fetchOptions } : {}),
      modelValue: '',
      'onUpdate:modelValue': onUpdate,
    },
  })
  return { wrapper, onUpdate }
}

async function openAndType(wrapper: ReturnType<typeof mount>, text: string) {
  await wrapper.get('button').trigger('click')
  await wrapper.get('input').setValue(text)
}

function optionTexts(wrapper: ReturnType<typeof mount>): string[] {
  return wrapper.findAll('[role="option"]').map((o) => o.text().replace(/\*$/, '').trim())
}

beforeEach(() => {
  vi.useFakeTimers()
})

afterEach(() => {
  vi.useRealTimers()
})

describe('FilterSelect server search', () => {
  it('looks typed text up on the server after the debounce and lists the results', async () => {
    const fetchOptions = vi.fn().mockResolvedValue([opt('core-1'), opt('core-10')])
    const { wrapper } = mountSelect(fetchOptions)

    await openAndType(wrapper, ' core-1 ')
    expect(fetchOptions).not.toHaveBeenCalled()
    expect(optionTexts(wrapper)).toEqual(['all', 'core-10'])

    vi.advanceTimersByTime(300)
    await flushPromises()
    expect(fetchOptions).toHaveBeenCalledExactlyOnceWith('core-1')
    expect(optionTexts(wrapper)).toEqual(['all', 'core-1', 'core-10'])
  })

  it('picks the server match on Enter before the debounce fires', async () => {
    const fetchOptions = vi.fn().mockResolvedValue([opt('core-1'), opt('core-10')])
    const { wrapper, onUpdate } = mountSelect(fetchOptions)

    await openAndType(wrapper, 'core-1')
    await wrapper.get('input').trigger('keydown', { key: 'Enter' })
    await flushPromises()

    expect(fetchOptions).toHaveBeenCalledWith('core-1')
    expect(onUpdate).toHaveBeenLastCalledWith('core-1')
  })

  it('keeps a * pattern local and offers it as free text', async () => {
    const fetchOptions = vi.fn().mockResolvedValue([])
    const { wrapper, onUpdate } = mountSelect(fetchOptions)

    await openAndType(wrapper, 'core-*')
    vi.advanceTimersByTime(300)
    await flushPromises()
    expect(fetchOptions).not.toHaveBeenCalled()

    await wrapper.get('input').trigger('keydown', { key: 'Enter' })
    await flushPromises()
    expect(onUpdate).toHaveBeenLastCalledWith('core-*')
  })

  it('falls back to the loaded options when the lookup fails', async () => {
    const fetchOptions = vi.fn().mockRejectedValue(new Error('offline'))
    const { wrapper } = mountSelect(fetchOptions)

    await openAndType(wrapper, 'a-')
    vi.advanceTimersByTime(300)
    await flushPromises()
    expect(optionTexts(wrapper)).toEqual(['all', 'a-000001', 'a-000002'])
  })

  it('matches the loaded options locally without fetchOptions', async () => {
    const { wrapper, onUpdate } = mountSelect()

    await openAndType(wrapper, 'core')
    expect(optionTexts(wrapper)).toEqual(['all', 'core-10'])
    await wrapper.get('input').trigger('keydown', { key: 'Enter' })
    expect(onUpdate).toHaveBeenLastCalledWith('core-10')
  })
})
