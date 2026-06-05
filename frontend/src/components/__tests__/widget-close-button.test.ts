// @vitest-environment jsdom
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import WidgetCloseButton from '../WidgetCloseButton.vue'

describe('WidgetCloseButton', () => {
  it('renders a single button with the close icon', () => {
    const wrapper = mount(WidgetCloseButton)
    expect(wrapper.findAll('button')).toHaveLength(1)
    expect(wrapper.find('svg').exists()).toBe(true)
  })

  it('emits close when clicked', async () => {
    const wrapper = mount(WidgetCloseButton)
    await wrapper.find('button').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
})
