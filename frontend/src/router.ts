import { createRouter, createWebHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { features } from '@/lib/features'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    {
      path: '/',
      name: 'home',
      component: () => import('@/views/HomeView.vue'),
    },
    // Netlog routes
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
    // Srvlog routes
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
    // Applog routes
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
    {
      path: '/notifications',
      name: 'notifications',
      component: () => import('@/views/NotificationsView.vue'),
    },
    // Analysis routes. Feature-gated, but the route *table* must not be: this
    // module is evaluated as part of the initial static import graph (stores/auth
    // imports the router, so the bundler hoists both into one statically-imported
    // chunk) — that is before main.ts awaits loadFeatures(). Branching the table
    // here baked in the fallback flags, leaving no route named 'analysis' even
    // when the feature was on, so nav to it threw MATCHER_NOT_FOUND. Reading the
    // flag inside these callbacks defers it to navigation time instead.
    {
      path: '/analysis',
      name: 'analysis',
      component: () =>
        features().analysis
          ? import('@/views/AnalysisView.vue')
          : import('@/views/FeatureDisabledView.vue'),
      props: () => (features().analysis ? {} : { feature: 'analysis' }),
    },
    {
      path: '/analysis/reports/:slug',
      name: 'analysis-report',
      component: () =>
        features().analysis
          ? import('@/views/AnalysisReportView.vue')
          : import('@/views/FeatureDisabledView.vue'),
      props: (to) => (features().analysis ? { slug: to.params.slug } : { feature: 'analysis' }),
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
      meta: { admin: true },
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

  // Admin-only routes: the backend is the authority (403s non-admins); this
  // guard is frontend defense-in-depth so non-admins never mount admin views.
  if (to.meta.admin && !auth.user?.is_admin) {
    return { name: 'home' }
  }
})

export default router
