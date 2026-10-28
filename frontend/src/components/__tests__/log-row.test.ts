// @vitest-environment jsdom
import { describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent, h, markRaw } from 'vue'
import LogRow from '../LogRow.vue'
import type { NetlogEvent } from '@/types/netlog'

// jsdom doesn't implement scrollIntoView; the row calls it on expand.
window.HTMLElement.prototype.scrollIntoView = vi.fn()

const DetailStub = markRaw(defineComponent({ setup: () => () => h('div', { class: 'detail' }) }))

function ev(overrides: Partial<NetlogEvent> = {}): NetlogEvent {
  return {
    id: 1,
    received_at: '2026-10-28T10:00:00Z',
    hostname: 'router1',
    programname: 'rpd',
    severity: 4,
    severity_label: 'warning',
    message: 'bundle ae30 flapped',
    ...overrides,
  } as NetlogEvent
}

function mountRow(event: NetlogEvent) {
  return mount(LogRow, {
    props: {
      event,
      detail: DetailStub,
      filterStore: { filters: { hostname: '' } },
      highlightPrefix: 'netlog',
    },
  })
}

describe('LogRow', () => {
  it('dims a muted row and prefixes its label, but not the expanded detail', async () => {
    const wrapper = mountRow(ev({ muted_by: 5 }))
    const rows = wrapper.findAll('[role="button"]')
    expect(rows).toHaveLength(2) // mobile + desktop layouts

    for (const row of rows) {
      expect(row.classes()).toContain('opacity-50')
      expect(row.attributes('aria-label')).toBe(
        'muted warning event from router1: bundle ae30 flapped',
      )
    }

    await rows[0]!.trigger('click')
    expect(wrapper.find('.detail').exists()).toBe(true)
    expect(wrapper.find('.group').classes()).not.toContain('opacity-50')
  })

  it('leaves an unmuted row as is', () => {
    const wrapper = mountRow(ev())
    for (const row of wrapper.findAll('[role="button"]')) {
      expect(row.classes()).not.toContain('opacity-50')
      expect(row.attributes('aria-label')).toBe('warning event from router1: bundle ae30 flapped')
    }
  })
})
