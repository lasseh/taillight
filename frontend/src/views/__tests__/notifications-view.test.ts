// @vitest-environment jsdom
import { describe, it, expect } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import NotificationsView from '../NotificationsView.vue'

const stubs = {
  NotificationChannels: true,
  NotificationRules: true,
  MuteRules: true,
  NotificationSummaries: true,
  NotificationLog: true,
}

async function mountAt(path: string) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/notifications', name: 'notifications', component: NotificationsView }],
  })
  await router.push(path)
  // Mounted directly, not through a RouterView, so a query-only push reuses
  // this instance the way the app reuses the routed view.
  const wrapper = mount(NotificationsView, { global: { plugins: [router], stubs } })
  return { wrapper, router }
}

function activeTab(wrapper: Awaited<ReturnType<typeof mountAt>>['wrapper']): string[] {
  return wrapper
    .findAll('button')
    .filter((b) => b.classes().includes('text-t-yellow'))
    .map((b) => b.text())
}

describe('NotificationsView tabs', () => {
  it('opens the tab named in ?tab=', async () => {
    const { wrapper } = await mountAt('/notifications?tab=mutes')
    expect(activeTab(wrapper)).toEqual(['Mutes'])
    expect(wrapper.findComponent({ name: 'MuteRules' }).exists()).toBe(true)
  })

  it('follows a query-only navigation on the same route', async () => {
    const { wrapper, router } = await mountAt('/notifications?tab=mutes')

    await router.push('/notifications')
    await flushPromises()
    expect(activeTab(wrapper)).toEqual(['Channels'])

    await router.push('/notifications?tab=log')
    await flushPromises()
    expect(activeTab(wrapper)).toEqual(['Log'])
  })

  it('writes the clicked tab to the query', async () => {
    const { wrapper, router } = await mountAt('/notifications')
    await wrapper
      .findAll('button')
      .find((b) => b.text() === 'Rules')!
      .trigger('click')
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ tab: 'rules' })
    expect(activeTab(wrapper)).toEqual(['Rules'])
  })
})
