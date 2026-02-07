<script setup lang="ts">
import { ref, watch, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useNotifications } from '@/composables/useNotifications'
import { useTheme } from '@/composables/useTheme'
import { useScrollStore } from '@/stores/scroll'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const scrollStore = useScrollStore()
const { supported, permission, enabled, requestPermission, setEnabled } = useNotifications()
const { themes, themeId, setTheme } = useTheme()

function navigateToLog(routeName: 'syslog' | 'applog') {
  scrollStore.requestScrollToBottom(routeName)
  router.push({ name: routeName })
}

const homeLink = '/'

const menuOpen = ref(false)
const menuRef = ref<HTMLElement | null>(null)
const mobileMenuRef = ref<HTMLElement | null>(null)
const mobileMenuOpen = ref(false)

function closeMobileMenu() {
  mobileMenuOpen.value = false
}

function mobileNavigateToLog(routeName: 'syslog' | 'applog') {
  closeMobileMenu()
  navigateToLog(routeName)
}

function pick(id: string) {
  setTheme(id)
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
              : 'text-t-fg-dark hover:text-t-blue'
          "
        >
          HOME
        </router-link>
        <button
          class="px-2 py-0.5 text-xs transition-colors"
          :class="
            String(route.name).startsWith('syslog')
              ? 'bg-t-bg-highlight text-t-teal'
              : 'text-t-fg-dark hover:text-t-teal'
          "
          @click="navigateToLog('syslog')"
        >
          SYSLOG
        </button>
        <button
          class="px-2 py-0.5 text-xs transition-colors"
          :class="
            String(route.name).startsWith('applog')
              ? 'bg-t-bg-highlight text-t-magenta'
              : 'text-t-fg-dark hover:text-t-magenta'
          "
          @click="navigateToLog('applog')"
        >
          APP LOG
        </button>
        <router-link
          :to="{ path: '/dashboard', query: { tab: String(route.name).startsWith('applog') ? 'applog' : 'syslog' } }"
          class="px-2 py-0.5 text-xs transition-colors"
          :class="
            route.name === 'dashboard'
              ? 'bg-t-bg-highlight text-t-purple'
              : 'text-t-fg-dark hover:text-t-purple'
          "
        >
          DASHBOARD
        </router-link>
      </div>

      <!-- Desktop: settings menu -->
      <div ref="menuRef" class="relative hidden md:block">
        <button
          class="text-t-fg-dark hover:text-t-fg flex items-center gap-1 px-1.5 py-0.5 text-xs transition-colors"
          :class="menuOpen || String(route.name).startsWith('settings') || route.name === 'api-keys' ? 'text-t-fg' : ''"
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
            <!-- User label -->
            <div class="border-t-border flex items-center gap-2 border-b px-3 py-2">
              <img
                v-if="auth.user?.gravatar_url"
                :src="auth.user.gravatar_url"
                alt=""
                class="h-5 w-5 rounded-full"
              />
              <span class="text-t-fg-dark text-xs">{{ auth.user?.username }}</span>
            </div>

            <!-- Settings section (authenticated only) -->
            <div v-if="auth.user?.username !== 'anonymous'" class="border-t-border border-b py-1">
              <span class="text-t-fg-dark px-3 py-1 text-[10px] font-semibold uppercase tracking-wider">Settings</span>
              <button
                class="text-t-fg-dark hover:bg-t-bg-hover hover:text-t-fg flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs transition-colors"
                @click="goToSettings"
              >
                <svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2" />
                  <circle cx="12" cy="7" r="4" />
                </svg>
                <span>User Settings</span>
              </button>
              <button
                class="text-t-fg-dark hover:bg-t-bg-hover hover:text-t-fg flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs transition-colors"
                @click="goToApiKeys"
              >
                <svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                  <path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4" />
                </svg>
                <span>API Keys</span>
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
            <div v-if="auth.user?.username !== 'anonymous'" class="border-t-border border-t py-1">
              <button
                class="text-t-fg-dark hover:bg-t-bg-hover hover:text-t-red flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs transition-colors"
                @click="menuOpen = false; auth.logout()"
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
                : 'text-t-fg-dark hover:text-t-blue'
            "
            @click="closeMobileMenu"
          >
            HOME
          </router-link>
          <button
            class="px-2 py-1.5 text-left text-xs transition-colors"
            :class="
              String(route.name).startsWith('syslog')
                ? 'bg-t-bg-highlight text-t-teal'
                : 'text-t-fg-dark hover:text-t-teal'
            "
            @click="mobileNavigateToLog('syslog')"
          >
            SYSLOG
          </button>
          <button
            class="px-2 py-1.5 text-left text-xs transition-colors"
            :class="
              String(route.name).startsWith('applog')
                ? 'bg-t-bg-highlight text-t-magenta'
                : 'text-t-fg-dark hover:text-t-magenta'
            "
            @click="mobileNavigateToLog('applog')"
          >
            APP LOG
          </button>
          <router-link
            :to="{ path: '/dashboard', query: { tab: String(route.name).startsWith('applog') ? 'applog' : 'syslog' } }"
            class="px-2 py-1.5 text-xs transition-colors"
            :class="
              route.name === 'dashboard'
                ? 'bg-t-bg-highlight text-t-purple'
                : 'text-t-fg-dark hover:text-t-purple'
            "
            @click="closeMobileMenu"
          >
            DASHBOARD
          </router-link>
        </div>

        <!-- User label -->
        <div class="bg-t-border my-3 h-px"></div>
        <div class="flex items-center gap-2 px-2 py-1">
          <img
            v-if="auth.user?.gravatar_url"
            :src="auth.user.gravatar_url"
            alt=""
            class="h-5 w-5 rounded-full"
          />
          <span class="text-t-fg-dark text-xs">{{ auth.user?.username }}</span>
        </div>

        <!-- Settings section (authenticated only) -->
        <template v-if="auth.user?.username !== 'anonymous'">
          <div class="bg-t-border my-3 h-px"></div>
          <span class="text-t-fg-dark px-2 py-1 text-[10px] font-semibold uppercase tracking-wider">Settings</span>
          <div class="flex flex-col gap-1">
            <button
              class="text-t-fg-dark hover:text-t-fg flex items-center gap-2 px-2 py-1.5 text-left text-xs transition-colors"
              @click="goToSettings"
            >
              <svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M19 21v-2a4 4 0 0 0-4-4H9a4 4 0 0 0-4 4v2" />
                <circle cx="12" cy="7" r="4" />
              </svg>
              <span>User Settings</span>
            </button>
            <button
              class="text-t-fg-dark hover:text-t-fg flex items-center gap-2 px-2 py-1.5 text-left text-xs transition-colors"
              @click="goToApiKeys"
            >
              <svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M21 2l-2 2m-7.61 7.61a5.5 5.5 0 1 1-7.778 7.778 5.5 5.5 0 0 1 7.777-7.777zm0 0L15.5 7.5m0 0l3 3L22 7l-3-3m-3.5 3.5L19 4" />
              </svg>
              <span>API Keys</span>
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
            v-if="auth.user?.username !== 'anonymous'"
            class="text-t-fg-dark hover:text-t-red flex items-center gap-1 text-xs transition-colors"
            @click="auth.logout(); closeMobileMenu()"
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
