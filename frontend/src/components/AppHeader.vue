<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter, type RouteLocationRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useNotifications } from '@/composables/useNotifications'
import { useTheme } from '@/composables/useTheme'
import { useScrollStore } from '@/stores/scroll'
import { useDashboardLayout } from '@/composables/useDashboardLayout'
import { features as getFeatures } from '@/lib/features'
import MenuItem from '@/components/MenuItem.vue'
import NavLink from '@/components/NavLink.vue'
import MenuIcon from '@/components/MenuIcon.vue'
import type { IconName } from '@/components/MenuIcon.vue'

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
  if (name.startsWith('netlog')) return 'netlog'
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

function closeMenus() {
  menuOpen.value = false
  closeMobileMenu()
}

function go(to: RouteLocationRaw) {
  closeMenus()
  router.push(to)
}

function pick(id: string) {
  setTheme(id)
}

function handleEditDashboard() {
  closeMenus()
  if (route.name !== 'home') {
    router.push('/')
  }
  startEditing()
}

async function handleLogout() {
  closeMenus()
  try {
    await auth.logout()
  } catch (e) {
    console.error('logout failed', e)
  }
}

// --- Nav data. Each entry (label, icon, action, visibility) is authored once
// and rendered by both the desktop and mobile menus.

/** DASHBOARD/NETLOG/SRVLOG/APPLOG strip. Feature flags are resolved before mount. */
interface FeedLink {
  id: 'home' | 'netlog' | 'srvlog' | 'applog'
  label: string
  title: string
  to?: string
  activeClass: string
  inactiveClass: string
}

const navLinks: FeedLink[] = [
  {
    id: 'home',
    label: 'DASHBOARD',
    title: 'Dashboard (1)',
    to: '/',
    activeClass: 'bg-t-bg-highlight text-t-blue',
    inactiveClass: 'text-t-blue/50 hover:text-t-blue',
  },
  {
    id: 'netlog',
    label: 'NETLOG',
    title: 'Netlog (2)',
    activeClass: 'bg-t-bg-highlight text-t-fuchsia',
    inactiveClass: 'text-t-fuchsia/50 hover:text-t-fuchsia',
  },
  {
    id: 'srvlog',
    label: 'SRVLOG',
    title: 'Srvlog (3)',
    activeClass: 'bg-t-bg-highlight text-t-teal',
    inactiveClass: 'text-t-teal/50 hover:text-t-teal',
  },
  {
    id: 'applog',
    label: 'APPLOG',
    title: 'Applog (4)',
    activeClass: 'bg-t-bg-highlight text-t-magenta',
    inactiveClass: 'text-t-magenta/50 hover:text-t-magenta',
  },
]

function isFeedActive(id: FeedLink['id']): boolean {
  if (id === 'home') return route.name === 'home'
  return String(route.name).startsWith(id)
}

function onFeedClick(link: FeedLink, mobile = false) {
  if (mobile) closeMobileMenu()
  if (link.id !== 'home') navigateToLog(link.id)
}

/** Settings-menu entries, grouped into the menus' bordered sections. */
interface MenuEntry {
  id: string
  label: string
  icon: IconName
  action: () => void
  show?: () => boolean
}

const menuSections: { id: string; items: MenuEntry[] }[] = [
  {
    id: 'general',
    items: [
      { id: 'hosts', label: 'Hosts', icon: 'hosts', action: () => go({ name: 'hosts' }) },
      {
        id: 'volume',
        label: 'Volume',
        icon: 'volume',
        action: () => go({ path: '/volume', query: { tab: volumeTab.value } }),
      },
      {
        id: 'alerts',
        label: 'Alerts',
        icon: 'alerts',
        action: () => go({ name: 'notifications' }),
      },
      {
        id: 'analysis',
        label: 'Analysis Reports',
        icon: 'analysis',
        action: () => go({ name: 'analysis' }),
        show: () => features.analysis,
      },
      {
        id: 'edit-dashboard',
        label: 'Edit Dashboard',
        icon: 'edit-dashboard',
        action: handleEditDashboard,
      },
    ],
  },
  {
    id: 'admin',
    items: [
      {
        id: 'api-keys',
        label: 'API Keys',
        icon: 'api-keys',
        action: () => go({ name: 'api-keys' }),
        show: () => isAuthenticated.value,
      },
      {
        id: 'users',
        label: 'Manage Users',
        icon: 'users',
        action: () => go({ name: 'admin-users' }),
        show: () => isAuthenticated.value && !!auth.user?.is_admin,
      },
    ],
  },
]

const visibleSections = computed(() =>
  menuSections
    .map((s) => ({ ...s, items: s.items.filter((i) => i.show?.() ?? true) }))
    .filter((s) => s.items.length > 0),
)

/** Logout is rendered outside the shared sections (own desktop section, mobile footer). */
const logoutItem: MenuEntry = {
  id: 'logout',
  label: 'Logout',
  icon: 'logout',
  action: handleLogout,
}

function onClickOutside(e: MouseEvent) {
  const target = e.target as Node
  const insideDesktop = menuRef.value?.contains(target)
  const insideMobile = mobileMenuRef.value?.contains(target)
  if (!insideDesktop && !insideMobile) {
    menuOpen.value = false
  }
}

watch(
  () => route.fullPath,
  () => {
    mobileMenuOpen.value = false
  },
)

onMounted(() => document.addEventListener('click', onClickOutside))
onUnmounted(() => document.removeEventListener('click', onClickOutside))
</script>

<template>
  <header class="border-t-border bg-t-bg-dark relative border-b">
    <div class="flex items-center gap-4 px-4 py-2">
      <router-link :to="homeLink" class="group font-semibold"
        ><span class="bg-gradient-to-r from-sev-emerg to-sev-alert bg-clip-text text-transparent"
          >[<span class="group-hover:underline">Taillight</span>]</span
        ></router-link
      >

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
        <NavLink
          v-for="l in navLinks"
          :key="l.id"
          :label="l.label"
          :title="l.title"
          :to="l.to"
          :active="isFeedActive(l.id)"
          :active-class="l.activeClass"
          :inactive-class="l.inactiveClass"
          @click="onFeedClick(l)"
        />
      </div>

      <!-- Desktop: settings menu -->
      <div ref="menuRef" class="relative hidden md:block">
        <button
          class="text-t-fg-dark hover:text-t-fg flex items-center gap-1 px-1.5 py-0.5 text-xs transition-colors"
          :class="
            menuOpen ||
            String(route.name).startsWith('settings') ||
            route.name === 'api-keys' ||
            route.name === 'analysis' ||
            route.name === 'admin-users' ||
            route.name === 'notifications' ||
            route.name === 'volume' ||
            route.name === 'hosts'
              ? 'text-t-fg'
              : ''
          "
          aria-label="Settings menu"
          aria-haspopup="true"
          :aria-expanded="menuOpen"
          @click.stop="menuOpen = !menuOpen"
        >
          <svg
            class="h-3.5 w-3.5"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
            focusable="false"
          >
            <path
              d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"
            />
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
              @click="go({ name: 'settings' })"
            >
              <img
                v-if="auth.user?.gravatar_url"
                :src="auth.user.gravatar_url"
                alt=""
                class="h-5 w-5 rounded-full"
              />
              <span class="text-t-fg-dark text-xs">{{ auth.user?.username }}</span>
            </button>

            <!-- Menu sections (admin section renders only when authenticated) -->
            <div
              v-for="section in visibleSections"
              :key="section.id"
              class="border-t-border border-b py-1"
            >
              <MenuItem
                v-for="item in section.items"
                :key="item.id"
                :icon="item.icon"
                :label="item.label"
                @click="item.action()"
              />
            </div>

            <!-- Themes section -->
            <div class="py-1">
              <span
                class="text-t-fg-dark px-3 py-1 text-[10px] font-semibold uppercase tracking-wider"
                >Theme</span
              >
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
              <MenuItem
                :icon="logoutItem.icon"
                :label="logoutItem.label"
                danger
                @click="logoutItem.action()"
              />
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
        <svg
          v-if="!mobileMenuOpen"
          class="h-5 w-5"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
        >
          <line x1="3" y1="6" x2="21" y2="6" />
          <line x1="3" y1="12" x2="21" y2="12" />
          <line x1="3" y1="18" x2="21" y2="18" />
        </svg>
        <!-- Close icon -->
        <svg
          v-else
          class="h-5 w-5"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          stroke-linecap="round"
          stroke-linejoin="round"
        >
          <line x1="18" y1="6" x2="6" y2="18" />
          <line x1="6" y1="6" x2="18" y2="18" />
        </svg>
      </button>
    </div>

    <!-- Mobile: dropdown panel -->
    <Transition name="mobile-menu">
      <div v-if="mobileMenuOpen" class="bg-t-bg-dark border-t-border border-t px-4 py-3 md:hidden">
        <!-- Nav links -->
        <div class="flex flex-col gap-1">
          <NavLink
            v-for="l in navLinks"
            :key="l.id"
            mobile
            :label="l.label"
            :to="l.to"
            :active="isFeedActive(l.id)"
            :active-class="l.activeClass"
            :inactive-class="l.inactiveClass"
            @click="onFeedClick(l, true)"
          />
        </div>

        <!-- User label (clickable → user settings) -->
        <div class="bg-t-border my-3 h-px"></div>
        <button
          class="hover:text-t-fg flex w-full items-center gap-2 px-2 py-1 text-left transition-colors"
          @click="go({ name: 'settings' })"
        >
          <img
            v-if="auth.user?.gravatar_url"
            :src="auth.user.gravatar_url"
            alt=""
            class="h-5 w-5 rounded-full"
          />
          <span class="text-t-fg-dark text-xs">{{ auth.user?.username }}</span>
        </button>

        <!-- Menu sections (admin section renders only when authenticated) -->
        <template v-for="section in visibleSections" :key="section.id">
          <div class="bg-t-border my-3 h-px"></div>
          <div class="flex flex-col gap-1">
            <MenuItem
              v-for="item in section.items"
              :key="item.id"
              mobile
              :icon="item.icon"
              :label="item.label"
              @click="item.action()"
            />
          </div>
        </template>

        <!-- Theme section -->
        <div class="bg-t-border my-3 h-px"></div>
        <span class="text-t-fg-dark px-2 py-1 text-[10px] font-semibold uppercase tracking-wider"
          >Theme</span
        >
        <div ref="mobileMenuRef" class="flex flex-col gap-0.5">
          <button
            v-for="t in themes"
            :key="t.id"
            class="flex items-center gap-2 px-2 py-1.5 text-left text-xs transition-colors"
            :class="
              t.id === themeId ? 'bg-t-bg-highlight text-t-fg' : 'text-t-fg-dark hover:text-t-fg'
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
            @click="logoutItem.action()"
          >
            <MenuIcon :name="logoutItem.icon" />
            <span>{{ logoutItem.label }}</span>
          </button>
        </div>
      </div>
    </Transition>
  </header>
</template>

<style scoped>
.menu-enter-active,
.menu-leave-active {
  transition:
    opacity 0.1s ease,
    transform 0.1s ease;
}

.menu-enter-from,
.menu-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

.mobile-menu-enter-active,
.mobile-menu-leave-active {
  transition:
    opacity 0.15s ease,
    transform 0.15s ease;
  transform-origin: top;
}

.mobile-menu-enter-from,
.mobile-menu-leave-to {
  opacity: 0;
  transform: scaleY(0.95);
}
</style>
