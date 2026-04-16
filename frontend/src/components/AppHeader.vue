<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useNotifications } from '@/composables/useNotifications'
import { useTheme } from '@/composables/useTheme'
import { useScrollStore } from '@/stores/scroll'
import { useDashboardLayout } from '@/composables/useDashboardLayout'
import { features as getFeatures } from '@/lib/features'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const scrollStore = useScrollStore()
const features = getFeatures()
const { supported, permission, enabled, requestPermission, setEnabled } = useNotifications()
const { themes, themeId, setTheme } = useTheme()
const { startEditing } = useDashboardLayout()

const isAuthenticated = computed(() => auth.user?.username !== 'anonymous')

function navigateToLog(routeName: 'netlog' | 'srvlog' | 'applog') {
  scrollStore.requestScrollToBottom(routeName)
  router.push({ name: routeName })
}

/** Determine the volume tab based on the current route context. */
const volumeTab = computed(() => {
  const name = String(route.name)
  if (name.startsWith('applog')) return 'applog'
  if (name.startsWith('netlog') && features.netlog) return 'netlog'
  return 'srvlog'
})

const homeLink = '/'

const menuOpen = ref(false)
const menuRef = ref<HTMLElement | null>(null)
const mobileMenuRef = ref<HTMLElement | null>(null)
const mobileMenuOpen = ref(false)

function closeMobileMenu() {
  mobileMenuOpen.value = false
}

function mobileNavigateToLog(routeName: 'netlog' | 'srvlog' | 'applog') {
  closeMobileMenu()
  navigateToLog(routeName)
}

function pick(id: string) {
  setTheme(id)
}

function goToUsers() {
  menuOpen.value = false
  closeMobileMenu()
  router.push({ name: 'admin-users' })
}

function goToSettings() {
  menuOpen.value = false
  closeMobileMenu()
  router.push({ name: 'settings' })
}

function goToApiKeys() {
  menuOpen.value = false
  closeMobileMenu()
  router.push({ name: 'api-keys' })
}

function goToAnalysis() {
  menuOpen.value = false
  closeMobileMenu()
  router.push({ name: 'analysis' })
}

function goToHosts() {
  menuOpen.value = false
  closeMobileMenu()
  router.push({ name: 'hosts' })
}

function goToVolume() {
  menuOpen.value = false
  closeMobileMenu()
  router.push({ path: '/volume', query: { tab: volumeTab.value } })
}

function goToNotifications() {
  menuOpen.value = false
  closeMobileMenu()
  router.push({ name: 'notifications' })
}

function handleEditDashboard() {
  menuOpen.value = false
  closeMobileMenu()
  if (route.name !== 'home') {
    router.push('/')
  }
  startEditing()
}

async function handleLogout() {
  menuOpen.value = false
  closeMobileMenu()
  try {
    await auth.logout()
  } catch (e) {
    console.error('logout failed', e)
  }
}

function onClickOutside(e: MouseEvent) {
  const target = e.target as Node
  const insideDesktop = menuRef.value?.contains(target)
  const insideMobile = mobileMenuRef.value?.contains(target)
  if (!insideDesktop && !insideMobile) {
    menuOpen.value = false
  }
}

watch(() => route.fullPath, () => {
  mobileMenuOpen.value = false
})

onMounted(() => document.addEventListener('click', onClickOutside))
onUnmounted(() => document.removeEventListener('click', onClickOutside))
</script>

<template>
  <header class="border-t-border bg-t-bg-dark relative border-b">
    <div class="flex items-center gap-4 px-4 py-2">
      <router-link :to="homeLink" class="group font-semibold"><span class="bg-gradient-to-r from-sev-emerg to-sev-alert bg-clip-text text-transparent">[<span class="group-hover:underline">Taillight</span>]</span></router-link>

      <div class="flex-1" />

      <!-- Desktop: notification toggle -->
      <button
        v-if="supported && permission !== 'granted'"
        class="text-t-yellow hidden text-xs md:inline"
        :title="permission === 'denied' ? 'Reset in browser site settings' : undefined"
        @click="requestPermission()"
      >
        {{ permission === 'denied' ? 'notifications blocked' : 'enable alerts' }}
      </button>
      <button
        v-else-if="supported && permission === 'granted'"
        class="hidden text-xs transition-colors md:inline"
        :class="enabled ? 'text-t-green hover:brightness-75' : 'text-t-red hover:brightness-75'"
        @click="setEnabled(!enabled)"
      >
        {{ enabled ? 'alerts on' : 'alerts off' }}
      </button>

      <!-- Desktop: nav links -->
      <div class="hidden gap-1 md:flex">
        <router-link
          to="/"
          class="px-2 py-0.5 text-xs transition-colors"
          :class="
            route.name === 'home'
              ? 'bg-t-bg-highlight text-t-blue'
              : 'text-t-blue/50 hover:text-t-blue'
          "
        >
          DASHBOARD
        </router-link>
        <button
          v-if="features.netlog"
          class="px-2 py-0.5 text-xs transition-colors"
          :class="
            String(route.name).startsWith('netlog')
              ? 'bg-t-bg-highlight text-t-fuchsia'
              : 'text-t-fuchsia/50 hover:text-t-fuchsia'
          "
          @click="navigateToLog('netlog')"
        >
          NETLOG
        </button>
        <button
          v-if="features.srvlog"
          class="px-2 py-0.5 text-xs transition-colors"
          :class="
            String(route.name).startsWith('srvlog')
              ? 'bg-t-bg-highlight text-t-teal'
              : 'text-t-teal/50 hover:text-t-teal'
          "
          @click="navigateToLog('srvlog')"
        >
          SRVLOG
        </button>
        <button
          v-if="features.applog"
          class="px-2 py-0.5 text-xs transition-colors"
          :class="
            String(route.name).startsWith('applog')
              ? 'bg-t-bg-highlight text-t-magenta'
              : 'text-t-magenta/50 hover:text-t-magenta'
          "
          @click="navigateToLog('applog')"
        >
          APPLOG
        </button>
      </div>

      <!-- Desktop: settings menu -->
      <div ref="menuRef" class="relative hidden md:block">
        <button
          class="text-t-fg-dark hover:text-t-fg flex items-center gap-1 px-1.5 py-0.5 text-xs transition-colors"
          :class="menuOpen || String(route.name).startsWith('settings') || route.name === 'api-keys' || route.name === 'analysis' || route.name === 'admin-users' || route.name === 'notifications' || route.name === 'volume' || route.name === 'hosts' ? 'text-t-fg' : ''"
          @click.stop="menuOpen = !menuOpen"
        >
          <svg class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
            <path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z" />
            <circle cx="12" cy="12" r="3" />
          </svg>
        </button>

        <Transition name="menu">
          <div
            v-if="menuOpen"
            class="bg-t-bg-dark border-t-border absolute right-0 top-full z-50 mt-1.5 min-w-60 rounded border shadow-lg"
          >
            <!-- User label (clickable → user settings) -->
            <button
              class="border-t-border hover:bg-t-bg-hover flex w-full items-center gap-2 border-b px-3 py-2 text-left transition-colors"
              @click="goToSettings"
            >
              <img
                v-if="auth.user?.gravatar_url"
                :src="auth.user.gravatar_url"
                alt=""
                class="h-5 w-5 rounded-full"
              />
              <span class="text-t-fg-dark text-xs">{{ auth.user?.username }}</span>
            </button>

            <!-- Menu items -->
            <div class="border-t-border border-b py-1">
              <button
                class="text-t-fg-dark hover:bg-t-bg-hover hover:text-t-fg flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs transition-colors"
                @click="goToHosts"
              >
                <svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <rect x="2" y="2" width="20" height="8" rx="2" ry="2" />
                  <rect x="2" y="14" width="20" height="8" rx="2" ry="2" />
                  <line x1="6" y1="6" x2="6.01" y2="6" />
                  <line x1="6" y1="18" x2="6.01" y2="18" />
                </svg>
                <span>Hosts</span>
              </button>
              <button
                class="text-t-fg-dark hover:bg-t-bg-hover hover:text-t-fg flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs transition-colors"
                @click="goToVolume"
              >
                <svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <line x1="18" y1="20" x2="18" y2="10" />
                  <line x1="12" y1="20" x2="12" y2="4" />
                  <line x1="6" y1="20" x2="6" y2="14" />
                </svg>
                <span>Volume</span>
              </button>
              <button
                class="text-t-fg-dark hover:bg-t-bg-hover hover:text-t-fg flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs transition-colors"
                @click="goToNotifications"
              >
                <svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9" />
                  <path d="M13.73 21a2 2 0 0 1-3.46 0" />
                </svg>
                <span>Alerts</span>
              </button>
              <button
                class="text-t-fg-dark hover:bg-t-bg-hover hover:text-t-fg flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs transition-colors"
                @click="goToAnalysis"
              >
                <svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
                  <polyline points="14 2 14 8 20 8" />
                  <line x1="16" y1="13" x2="8" y2="13" />
                  <line x1="16" y1="17" x2="8" y2="17" />
                </svg>
                <span>Analysis Reports</span>
              </button>
              <button
                class="text-t-fg-dark hover:bg-t-bg-hover hover:text-t-fg flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs transition-colors"
                @click="handleEditDashboard"
              >
                <svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <rect x="3" y="3" width="7" height="7" />
                  <rect x="14" y="3" width="7" height="7" />
                  <rect x="3" y="14" width="7" height="7" />
                  <rect x="14" y="14" width="7" height="7" />
                </svg>
                <span>Edit Dashboard</span>
              </button>
            </div>

            <!-- Admin items (authenticated only) -->
            <div v-if="isAuthenticated" class="border-t-border border-b py-1">
              <button
                class="text-t-fg-dark hover:bg-t-bg-hover hover:text-t-fg flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs transition-colors"
                @click="goToApiKeys"
              >
                <svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4" />
                </svg>
                <span>API Keys</span>
              </button>
              <button
                v-if="auth.user?.is_admin"
                class="text-t-fg-dark hover:bg-t-bg-hover hover:text-t-fg flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs transition-colors"
                @click="goToUsers"
              >
                <svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2" />
                  <circle cx="9" cy="7" r="4" />
                  <path d="M23 21v-2a4 4 0 0 0-3-3.87" />
                  <path d="M16 3.13a4 4 0 0 1 0 7.75" />
                </svg>
                <span>Manage Users</span>
              </button>
            </div>

            <!-- Themes section -->
            <div class="py-1">
              <span class="text-t-fg-dark px-3 py-1 text-[10px] font-semibold uppercase tracking-wider">Theme</span>
              <button
                v-for="t in themes"
                :key="t.id"
                class="flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs transition-colors"
                :class="
                  t.id === themeId
                    ? 'bg-t-bg-highlight text-t-fg'
                    : 'text-t-fg-dark hover:bg-t-bg-hover hover:text-t-fg'
                "
                @click="pick(t.id)"
              >
                <span class="flex gap-0.5">
                  <span
                    v-for="(c, ci) in t.chartColors.slice(0, 4)"
                    :key="ci"
                    class="inline-block h-2 w-2 rounded-full"
                    :style="{ backgroundColor: c }"
                  />
                </span>
                <span>{{ t.name }}</span>
                <span v-if="t.id === themeId" class="text-t-green ml-auto">*</span>
              </button>
            </div>

            <!-- Logout (authenticated only) -->
            <div v-if="isAuthenticated" class="border-t-border border-t py-1">
              <button
                class="text-t-fg-dark hover:bg-t-bg-hover hover:text-t-red flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs transition-colors"
                @click="handleLogout"
              >
                <svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
                  <polyline points="16 17 21 12 16 7" />
                  <line x1="21" y1="12" x2="9" y2="12" />
                </svg>
                <span>Logout</span>
              </button>
            </div>
          </div>
        </Transition>
      </div>

      <!-- Mobile: hamburger button -->
      <button
        aria-label="Toggle menu"
        :aria-expanded="mobileMenuOpen"
        class="text-t-fg-dark hover:text-t-fg p-1 md:hidden"
        @click="mobileMenuOpen = !mobileMenuOpen"
      >
        <!-- Hamburger icon -->
        <svg v-if="!mobileMenuOpen" class="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <line x1="3" y1="6" x2="21" y2="6" />
          <line x1="3" y1="12" x2="21" y2="12" />
          <line x1="3" y1="18" x2="21" y2="18" />
        </svg>
        <!-- Close icon -->
        <svg v-else class="h-5 w-5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <line x1="18" y1="6" x2="6" y2="18" />
          <line x1="6" y1="6" x2="18" y2="18" />
        </svg>
      </button>
    </div>

    <!-- Mobile: dropdown panel -->
    <Transition name="mobile-menu">
      <div
        v-if="mobileMenuOpen"
        class="bg-t-bg-dark border-t-border border-t px-4 py-3 md:hidden"
      >
        <!-- Nav links -->
        <div class="flex flex-col gap-1">
          <router-link
            to="/"
            class="px-2 py-1.5 text-xs transition-colors"
            :class="
              route.name === 'home'
                ? 'bg-t-bg-highlight text-t-blue'
                : 'text-t-blue/50 hover:text-t-blue'
            "
            @click="closeMobileMenu"
          >
            DASHBOARD
          </router-link>
          <button
            v-if="features.netlog"
            class="px-2 py-1.5 text-left text-xs transition-colors"
            :class="
              String(route.name).startsWith('netlog')
                ? 'bg-t-bg-highlight text-t-fuchsia'
                : 'text-t-fuchsia/50 hover:text-t-fuchsia'
            "
            @click="mobileNavigateToLog('netlog')"
          >
            NETLOG
          </button>
          <button
            v-if="features.srvlog"
            class="px-2 py-1.5 text-left text-xs transition-colors"
            :class="
              String(route.name).startsWith('srvlog')
                ? 'bg-t-bg-highlight text-t-teal'
                : 'text-t-teal/50 hover:text-t-teal'
            "
            @click="mobileNavigateToLog('srvlog')"
          >
            SRVLOG
          </button>
          <button
            v-if="features.applog"
            class="px-2 py-1.5 text-left text-xs transition-colors"
            :class="
              String(route.name).startsWith('applog')
                ? 'bg-t-bg-highlight text-t-magenta'
                : 'text-t-magenta/50 hover:text-t-magenta'
            "
            @click="mobileNavigateToLog('applog')"
          >
            APPLOG
          </button>
        </div>

        <!-- User label (clickable → user settings) -->
        <div class="bg-t-border my-3 h-px"></div>
        <button
          class="hover:text-t-fg flex w-full items-center gap-2 px-2 py-1 text-left transition-colors"
          @click="goToSettings"
        >
          <img
            v-if="auth.user?.gravatar_url"
            :src="auth.user.gravatar_url"
            alt=""
            class="h-5 w-5 rounded-full"
          />
          <span class="text-t-fg-dark text-xs">{{ auth.user?.username }}</span>
        </button>

        <!-- Menu items -->
        <div class="bg-t-border my-3 h-px"></div>
        <div class="flex flex-col gap-1">
          <button
            class="text-t-fg-dark hover:text-t-fg flex items-center gap-2 px-2 py-1.5 text-left text-xs transition-colors"
            @click="goToHosts"
          >
            <svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <rect x="2" y="2" width="20" height="8" rx="2" ry="2" />
              <rect x="2" y="14" width="20" height="8" rx="2" ry="2" />
              <line x1="6" y1="6" x2="6.01" y2="6" />
              <line x1="6" y1="18" x2="6.01" y2="18" />
            </svg>
            <span>Hosts</span>
          </button>
          <button
            class="text-t-fg-dark hover:text-t-fg flex items-center gap-2 px-2 py-1.5 text-left text-xs transition-colors"
            @click="goToVolume"
          >
            <svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <line x1="18" y1="20" x2="18" y2="10" />
              <line x1="12" y1="20" x2="12" y2="4" />
              <line x1="6" y1="20" x2="6" y2="14" />
            </svg>
            <span>Volume</span>
          </button>
          <button
            class="text-t-fg-dark hover:text-t-fg flex items-center gap-2 px-2 py-1.5 text-left text-xs transition-colors"
            @click="goToNotifications"
          >
            <svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9" />
              <path d="M13.73 21a2 2 0 0 1-3.46 0" />
            </svg>
            <span>Alerts</span>
          </button>
          <button
            class="text-t-fg-dark hover:text-t-fg flex items-center gap-2 px-2 py-1.5 text-left text-xs transition-colors"
            @click="goToAnalysis"
          >
            <svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
              <polyline points="14 2 14 8 20 8" />
              <line x1="16" y1="13" x2="8" y2="13" />
              <line x1="16" y1="17" x2="8" y2="17" />
            </svg>
            <span>Analysis Reports</span>
          </button>
          <button
            class="text-t-fg-dark hover:text-t-fg flex items-center gap-2 px-2 py-1.5 text-left text-xs transition-colors"
            @click="handleEditDashboard"
          >
            <svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <rect x="3" y="3" width="7" height="7" />
              <rect x="14" y="3" width="7" height="7" />
              <rect x="3" y="14" width="7" height="7" />
              <rect x="14" y="14" width="7" height="7" />
            </svg>
            <span>Edit Dashboard</span>
          </button>
        </div>

        <!-- Admin items (authenticated only) -->
        <template v-if="isAuthenticated">
          <div class="bg-t-border my-3 h-px"></div>
          <div class="flex flex-col gap-1">
            <button
              class="text-t-fg-dark hover:text-t-fg flex items-center gap-2 px-2 py-1.5 text-left text-xs transition-colors"
              @click="goToApiKeys"
            >
              <svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4" />
              </svg>
              <span>API Keys</span>
            </button>
            <button
              v-if="auth.user?.is_admin"
              class="text-t-fg-dark hover:text-t-fg flex items-center gap-2 px-2 py-1.5 text-left text-xs transition-colors"
              @click="goToUsers"
            >
              <svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2" />
                <circle cx="9" cy="7" r="4" />
                <path d="M23 21v-2a4 4 0 0 0-3-3.87" />
                <path d="M16 3.13a4 4 0 0 1 0 7.75" />
              </svg>
              <span>Manage Users</span>
            </button>
          </div>
        </template>

        <!-- Theme section -->
        <div class="bg-t-border my-3 h-px"></div>
        <span class="text-t-fg-dark px-2 py-1 text-[10px] font-semibold uppercase tracking-wider">Theme</span>
        <div ref="mobileMenuRef" class="flex flex-col gap-0.5">
          <button
            v-for="t in themes"
            :key="t.id"
            class="flex items-center gap-2 px-2 py-1.5 text-left text-xs transition-colors"
            :class="
              t.id === themeId
                ? 'bg-t-bg-highlight text-t-fg'
                : 'text-t-fg-dark hover:text-t-fg'
            "
            @click="pick(t.id)"
          >
            <span class="flex gap-0.5">
              <span
                v-for="(c, ci) in t.chartColors.slice(0, 4)"
                :key="ci"
                class="inline-block h-2 w-2 rounded-full"
                :style="{ backgroundColor: c }"
              />
            </span>
            <span>{{ t.name }}</span>
            <span v-if="t.id === themeId" class="text-t-green ml-auto">*</span>
          </button>
        </div>

        <!-- Alerts & logout -->
        <div class="bg-t-border my-3 h-px"></div>
        <div class="flex items-center gap-3">
          <button
            v-if="supported && permission !== 'granted'"
            class="text-t-yellow text-xs"
            @click="requestPermission()"
          >
            {{ permission === 'denied' ? 'notifications blocked' : 'enable alerts' }}
          </button>
          <button
            v-else-if="supported && permission === 'granted'"
            class="text-xs transition-colors"
            :class="enabled ? 'text-t-green hover:brightness-75' : 'text-t-red hover:brightness-75'"
            @click="setEnabled(!enabled)"
          >
            {{ enabled ? 'alerts on' : 'alerts off' }}
          </button>
          <div class="flex-1" />
          <button
            v-if="isAuthenticated"
            class="text-t-fg-dark hover:text-t-red flex items-center gap-1 text-xs transition-colors"
            @click="handleLogout"
          >
            <svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
              <polyline points="16 17 21 12 16 7" />
              <line x1="21" y1="12" x2="9" y2="12" />
            </svg>
            <span>Logout</span>
          </button>
        </div>
      </div>
    </Transition>
  </header>
</template>

<style scoped>
.menu-enter-active,
.menu-leave-active {
  transition: opacity 0.1s ease, transform 0.1s ease;
}

.menu-enter-from,
.menu-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

.mobile-menu-enter-active,
.mobile-menu-leave-active {
  transition: opacity 0.15s ease, transform 0.15s ease;
  transform-origin: top;
}

.mobile-menu-enter-from,
.mobile-menu-leave-to {
  opacity: 0;
  transform: scaleY(0.95);
}
</style>
