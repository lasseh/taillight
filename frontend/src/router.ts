import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      name: 'home',
      component: () => import('@/views/HomeView.vue'),
    },
    {
      path: '/syslog',
      name: 'syslog',
      component: () => import('@/views/SyslogListView.vue'),
    },
    {
      path: '/dashboard',
      name: 'dashboard',
      component: () => import('@/views/DashboardView.vue'),
    },
    {
      path: '/syslog/:id',
      name: 'syslog-detail',
      component: () => import('@/views/SyslogView.vue'),
      props: true,
    },
    {
      path: '/applog',
      name: 'applog',
      component: () => import('@/views/AppLogListView.vue'),
    },
    {
      path: '/applog/:id',
      name: 'applog-detail',
      component: () => import('@/views/AppLogView.vue'),
      props: true,
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
    return { name: 'login' }
  }

  if (auth.user && to.name === 'login') {
    return { name: 'home' }
  }
})

export default router
