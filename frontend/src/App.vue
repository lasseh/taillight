<script setup lang="ts">
import { ref, computed, watch, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { useMetaStore } from '@/stores/meta'
import { useSyslogFilterStore } from '@/stores/syslog-filters'
import { useAppLogFilterStore } from '@/stores/applog-filters'
import { useAppLogMetaStore } from '@/stores/applog-meta'
import { useScrollStore } from '@/stores/scroll'
import { useSyslogStream } from '@/composables/useSyslogStream'
import { useAppLogStream } from '@/composables/useAppLogStream'
import { useNotifications } from '@/composables/useNotifications'
import { useFavicon } from '@/composables/useFavicon'
import AppHeader from '@/components/AppHeader.vue'
import FilterBar from '@/components/FilterBar.vue'
import AppLogFilterBar from '@/components/AppLogFilterBar.vue'
import StatusBadge from '@/components/StatusBadge.vue'

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()
const meta = useMetaStore()
const filters = useSyslogFilterStore()
const appLogFilters = useAppLogFilterStore()
const appLogMeta = useAppLogMetaStore()
const scrollStore = useScrollStore()

// Wait for initial navigation to complete before rendering the layout.
// This prevents a race where auth resolves (triggering layout render)
// before the router confirms the route (leaving <router-view> empty).
const routerReady = ref(false)
router.isReady().then(() => { routerReady.value = true })

const isLoginRoute = computed(() => route.name === 'login')
const isLogRoute = computed(() => route.name === 'syslog' || route.name === 'applog')
const showJumpToLatest = computed(() => {
  if (!isLogRoute.value) return false
  return !scrollStore.isPinned(String(route.name))
})

const newEventCount = computed(() => scrollStore.getNewEventCount(String(route.name)))

const syslogStream = useSyslogStream()
const applogStream = useAppLogStream()
const { notifySyslog, notifyApplog } = useNotifications()

const connected = computed(() => syslogStream.connected.value || applogStream.connected.value)

const isHistoricalMode = computed(() => {
  if (route.name === 'syslog') return Boolean(filters.filters.from || filters.filters.to)
  if (route.name === 'applog') return Boolean(appLogFilters.filters.from || appLogFilters.filters.to)
  return false
})

useFavicon(connected)

let unsubSyslog: (() => void) | null = null
let unsubApplog: (() => void) | null = null

function startStreams() {
  filters.initFromURL()
  appLogFilters.initFromURL()
  meta.fetchAll()
  appLogMeta.fetchAll()
  syslogStream.start()
  applogStream.start()
  unsubSyslog = syslogStream.subscribe(notifySyslog)
  unsubApplog = applogStream.subscribe(notifyApplog)
}

function stopStreams() {
  unsubSyslog?.()
  unsubApplog?.()
  unsubSyslog = null
  unsubApplog = null
  syslogStream.stop()
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
</script>

<template>
  <router-view v-if="isLoginRoute" />
  <div v-else-if="routerReady && auth.user" class="flex h-screen flex-col">
    <AppHeader />
    <FilterBar v-if="route.name === 'syslog'" />
    <AppLogFilterBar v-if="route.name === 'applog'" />
    <router-view v-slot="{ Component }">
      <KeepAlive include="SyslogListView,AppLogListView">
        <component :is="Component" />
      </KeepAlive>
    </router-view>
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
        paused{{ newEventCount > 0 ? ` · ${newEventCount} new` : '' }} — ↓ jump to latest (esc)
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
