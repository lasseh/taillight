// @vitest-environment jsdom
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import RangePresets from '../RangePresets.vue'
import { rangePresets } from '@/lib/ranges'

describe('RangePresets', () => {
  it('renders one button per preset', () => {
    const wrapper = mount(RangePresets, { props: { range: '24h' } })
    expect(wrapper.findAll('button')).toHaveLength(rangePresets.length)
  })

  it('marks the active range with the highlight class', () => {
    const wrapper = mount(RangePresets, { props: { range: '6h' } })
    const active = wrapper.findAll('button').find((b) => b.text() === '6h')!
    expect(active.classes()).toContain('text-t-purple')
  })

  it('emits select with the preset value on click', async () => {
    const wrapper = mount(RangePresets, { props: { range: '24h' } })
    const sevenDay = wrapper.findAll('button').find((b) => b.text() === '7d')!
    await sevenDay.trigger('click')
    expect(wrapper.emitted('select')).toEqual([['7d']])
  })
})
