import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { features } from '@/config'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      name: 'home',
      component: () => import('@/views/HomeView.vue'),
    },
    // Netlog routes (feature-gated)
    ...(features.netlog
      ? [
          {
            path: '/netlog',
            name: 'netlog',
            component: () => import('@/views/NetlogListView.vue'),
          },
          {
            path: '/netlog/device/:hostname',
            name: 'netlog-device-detail',
            component: () => import('@/views/NetlogDeviceView.vue'),
            props: true,
          },
          {
            path: '/netlog/:id',
            name: 'netlog-detail',
            component: () => import('@/views/NetlogView.vue'),
            props: true,
          },
        ]
      : [
          {
            path: '/netlog/:pathMatch(.*)*',
            name: 'netlog-disabled',
            component: () => import('@/views/FeatureDisabledView.vue'),
            props: { feature: 'netlog' },
          },
        ]),
    // Srvlog routes (feature-gated)
    ...(features.srvlog
      ? [
          {
            path: '/srvlog',
            name: 'srvlog',
            component: () => import('@/views/SrvlogListView.vue'),
          },
          {
            path: '/srvlog/device/:hostname',
            name: 'srvlog-device-detail',
            component: () => import('@/views/DeviceView.vue'),
            props: true,
          },
          {
            path: '/srvlog/:id',
            name: 'srvlog-detail',
            component: () => import('@/views/SrvlogView.vue'),
            props: true,
          },
        ]
      : [
          {
            path: '/srvlog/:pathMatch(.*)*',
            name: 'srvlog-disabled',
            component: () => import('@/views/FeatureDisabledView.vue'),
            props: { feature: 'srvlog' },
          },
        ]),
    {
      path: '/hosts',
      name: 'hosts',
      component: () => import('@/views/HostsView.vue'),
    },
    {
      path: '/volume',
      name: 'volume',
      component: () => import('@/views/VolumeView.vue'),
    },
    // Applog routes (feature-gated)
    ...(features.applog
      ? [
          {
            path: '/applog',
            name: 'applog',
            component: () => import('@/views/AppLogListView.vue'),
          },
          {
            path: '/applog/device/:hostname',
            name: 'applog-device-detail',
            component: () => import('@/views/AppLogDeviceView.vue'),
            props: true,
          },
          {
            path: '/applog/:id',
            name: 'applog-detail',
            component: () => import('@/views/AppLogView.vue'),
            props: true,
          },
        ]
      : [
          {
            path: '/applog/:pathMatch(.*)*',
            name: 'applog-disabled',
            component: () => import('@/views/FeatureDisabledView.vue'),
            props: { feature: 'applog' },
          },
        ]),
    {
      path: '/notifications',
      name: 'notifications',
      component: () => import('@/views/NotificationsView.vue'),
      meta: { public: true },
    },
    {
      path: '/analysis',
      name: 'analysis',
      component: () => import('@/views/AnalysisView.vue'),
    },
    {
      path: '/settings',
      name: 'settings',
      component: () => import('@/views/SettingsView.vue'),
    },
    {
      path: '/settings/api-keys',
      name: 'api-keys',
      component: () => import('@/views/ApiKeysView.vue'),
    },
    {
      path: '/admin/users',
      name: 'admin-users',
      component: () => import('@/views/UsersView.vue'),
    },
    {
      path: '/login',
      name: 'login',
      component: () => import('@/views/LoginView.vue'),
      meta: { public: true },
    },
    {
      path: '/:pathMatch(.*)*',
      name: 'not-found',
      component: () => import('@/views/NotFoundView.vue'),
      meta: { public: true },
    },
  ],
})

router.beforeEach(async (to) => {
  const auth = useAuthStore()

  // Wait for initial auth check to complete.
  if (!auth.ready) {
    await auth.init()
  }

  if (!auth.user && !to.meta.public) {
    return { name: 'login', query: { redirect: to.fullPath } }
  }

  if (auth.user && to.name === 'login') {
    return { name: 'home' }
  }
})

export default router
