<script setup lang="ts">
import { ref, computed, watch, onUnmounted, onErrorCaptured } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useMetaStore } from '@/stores/meta'
import { useSrvlogFilterStore } from '@/stores/srvlog-filters'
import { useNetlogFilterStore } from '@/stores/netlog-filters'
import { useAppLogFilterStore } from '@/stores/applog-filters'
import { useAppLogMetaStore } from '@/stores/applog-meta'
import { useNetlogMetaStore } from '@/stores/netlog-meta'
import { useScrollStore } from '@/stores/scroll'
import { useSrvlogStream } from '@/composables/useSrvlogStream'
import { useNetlogStream } from '@/composables/useNetlogStream'
import { useAppLogStream } from '@/composables/useAppLogStream'
import { useNotifications } from '@/composables/useNotifications'
import { useFavicon } from '@/composables/useFavicon'
import { useFullscreen } from '@/composables/useFullscreen'
import { useColumnVisibility } from '@/composables/useColumnVisibility'
import { useFeaturesStore } from '@/stores/features'
import AppHeader from '@/components/AppHeader.vue'
import FilterBar from '@/components/FilterBar.vue'
import NetlogFilterBar from '@/components/NetlogFilterBar.vue'
import AppLogFilterBar from '@/components/AppLogFilterBar.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import ConnectionBanner from '@/components/ConnectionBanner.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const meta = useMetaStore()
const filters = useSrvlogFilterStore()
const netlogFilters = useNetlogFilterStore()
const appLogFilters = useAppLogFilterStore()
const appLogMeta = useAppLogMetaStore()
const netlogMeta = useNetlogMetaStore()
const scrollStore = useScrollStore()
const { features } = useFeaturesStore()

// Wait for initial navigation to complete before rendering the layout.
// This prevents a race where auth resolves (triggering layout render)
// before the router confirms the route (leaving <router-view> empty).
const routerReady = ref(false)
router.isReady().then(() => { routerReady.value = true })

const isLoginRoute = computed(() => route.name === 'login')
const isLogRoute = computed(() => route.name === 'netlog' || route.name === 'srvlog' || route.name === 'applog')
const showJumpToLatest = computed(() => {
  if (!isLogRoute.value) return false
  return !scrollStore.isPinned(String(route.name))
})

const newEventCount = computed(() => scrollStore.getNewEventCount(String(route.name)))

const srvlogStream = useSrvlogStream()
const netlogStream = useNetlogStream()
const applogStream = useAppLogStream()
const { notifySrvlog, notifyNetlog, notifyApplog } = useNotifications()

const connected = computed(() => srvlogStream.connected.value || netlogStream.connected.value || applogStream.connected.value)

const isHistoricalMode = computed(() => {
  if (route.name === 'netlog') return Boolean(netlogFilters.filters.from || netlogFilters.filters.to)
  if (route.name === 'srvlog') return Boolean(filters.filters.from || filters.filters.to)
  if (route.name === 'applog') return Boolean(appLogFilters.filters.from || appLogFilters.filters.to)
  return false
})

useFavicon(connected)

const { active: fullscreenActive, exit: exitFullscreen, toggle: toggleFullscreen } = useFullscreen()

// Program column toggle: only meaningful on routes that render a program column.
const programColumnRoute = computed(() => {
  if (route.name === 'srvlog') return 'srvlog'
  if (route.name === 'netlog') return 'netlog'
  return null
})
const srvlogProgramCol = useColumnVisibility('srvlog', 'program')
const netlogProgramCol = useColumnVisibility('netlog', 'program')
const programColumn = computed(() => {
  if (programColumnRoute.value === 'srvlog') return srvlogProgramCol
  if (programColumnRoute.value === 'netlog') return netlogProgramCol
  return null
})

// Auto-exit fullscreen when navigating away from log routes.
watch(isLogRoute, (isLog) => {
  if (!isLog && fullscreenActive.value) {
    exitFullscreen()
  }
})

let unsubSrvlog: (() => void) | null = null
let unsubNetlog: (() => void) | null = null
let unsubApplog: (() => void) | null = null

function startStreams() {
  filters.initFromURL()
  netlogFilters.initFromURL()
  appLogFilters.initFromURL()
  meta.fetchAll()
  if (features.netlog) netlogMeta.fetchAll()
  appLogMeta.fetchAll()
  srvlogStream.start()
  if (features.netlog) netlogStream.start()
  applogStream.start()
  unsubSrvlog = srvlogStream.subscribe(notifySrvlog)
  if (features.netlog) unsubNetlog = netlogStream.subscribe(notifyNetlog)
  unsubApplog = applogStream.subscribe(notifyApplog)
}

function stopStreams() {
  unsubSrvlog?.()
  unsubNetlog?.()
  unsubApplog?.()
  unsubSrvlog = null
  unsubNetlog = null
  unsubApplog = null
  srvlogStream.stop()
  netlogStream.stop()
  applogStream.stop()
}

// Start/stop streams based on auth state, but wait for router to be ready
// so that initFromURL() can read query params from the resolved route.
router.isReady().then(() => {
  watch(
    () => auth.user,
    (u) => {
      if (u) {
        startStreams()
      } else {
        stopStreams()
      }
    },
    { immediate: true },
  )
})

onUnmounted(() => {
  stopStreams()
})

// Global error boundary — catches uncaught errors from child components.
const fatalError = ref<string | null>(null)
onErrorCaptured((err) => {
  fatalError.value = err instanceof Error ? err.message : String(err)
  console.error('Uncaught component error:', err)
  return false
})
</script>

<template>
  <div v-if="fatalError" class="flex h-dvh items-center justify-center bg-neutral-900 text-neutral-200">
    <div class="max-w-md space-y-4 text-center">
      <h1 class="text-xl font-semibold text-red-400">Something went wrong</h1>
      <p class="text-sm text-neutral-400">{{ fatalError }}</p>
      <button class="rounded bg-neutral-700 px-4 py-2 text-sm hover:bg-neutral-600" @click="fatalError = null">
        Try again
      </button>
    </div>
  </div>
  <router-view v-else-if="isLoginRoute" />
  <div v-else-if="routerReady && auth.user" class="flex h-dvh flex-col">
    <template v-if="!fullscreenActive">
      <AppHeader />
      <NetlogFilterBar v-if="route.name === 'netlog'" />
      <FilterBar v-if="route.name === 'srvlog'" />
      <AppLogFilterBar v-if="route.name === 'applog'" />
    </template>
    <ConnectionBanner :connected="connected" />
    <main class="flex min-h-0 flex-1 flex-col">
      <router-view v-slot="{ Component }">
        <KeepAlive include="NetlogListView,SrvlogListView,AppLogListView">
          <component :is="Component" />
        </KeepAlive>
      </router-view>
    </main>
    <div class="border-t-border bg-t-bg-dark relative flex items-center border-t px-4 py-1.5">
      <span
        v-if="isHistoricalMode"
        role="status"
        aria-label="Viewing historical data"
        class="bg-t-yellow/15 text-t-yellow inline-flex items-center gap-1.5 rounded px-1.5 py-0.5 text-xs"
      >
        <span class="bg-t-yellow inline-block h-1.5 w-1.5 rounded-full" aria-hidden="true" />
        historical
      </span>
      <StatusBadge v-else :connected="connected" />
      <button
        v-if="showJumpToLatest"
        class="text-t-magenta hover:text-t-fg absolute left-1/2 -translate-x-1/2 text-xs animate-subtle-pulse transition-colors"
        @click="scrollStore.triggerJump(String(route.name))"
      >
        <span class="hidden md:inline">auto-scroll off{{ newEventCount > 0 ? ` · ${newEventCount} new` : '' }} — ↓ jump to latest (esc)</span>
        <span class="md:hidden">↓ latest{{ newEventCount > 0 ? ` (${newEventCount})` : '' }}</span>
      </button>
      <button
        v-if="programColumn"
        class="text-t-fg-dark hover:text-t-fg ml-auto p-1 transition-colors"
        :aria-label="programColumn.visible.value ? 'Hide program column' : 'Show program column'"
        :title="programColumn.visible.value ? 'Hide program column' : 'Show program column'"
        @click="programColumn.toggle()"
      >
        <!-- Visible → inward chevrons ><  hint: click to collapse -->
        <svg v-if="programColumn.visible.value" class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <polyline points="5 6 9 12 5 18" />
          <polyline points="19 6 15 12 19 18" />
        </svg>
        <!-- Hidden → outward chevrons <>  hint: click to expand -->
        <svg v-else class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <polyline points="9 6 5 12 9 18" />
          <polyline points="15 6 19 12 15 18" />
        </svg>
      </button>
      <button
        v-if="isLogRoute"
        class="text-t-fg-dark hover:text-t-fg p-1 transition-colors"
        :class="{ 'ml-auto': !programColumn }"
        :aria-label="fullscreenActive ? 'Exit focus mode (f)' : 'Focus mode (f)'"
        :title="fullscreenActive ? 'Exit focus mode (f)' : 'Focus mode (f)'"
        @click="toggleFullscreen()"
      >
        <svg v-if="!fullscreenActive" class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M8 3H5a2 2 0 0 0-2 2v3" />
          <path d="M21 8V5a2 2 0 0 0-2-2h-3" />
          <path d="M3 16v3a2 2 0 0 0 2 2h3" />
          <path d="M16 21h3a2 2 0 0 0 2-2v-3" />
        </svg>
        <svg v-else class="h-3.5 w-3.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M4 14h6v6" />
          <path d="M20 10h-6V4" />
          <path d="M14 10l7-7" />
          <path d="M3 21l7-7" />
        </svg>
      </button>
    </div>
  </div>
</template>

<style>
@keyframes subtle-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.5; }
}
.animate-subtle-pulse {
  animation: subtle-pulse 3s ease-in-out infinite;
}
</style>
