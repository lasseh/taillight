<script setup lang="ts">
import { ref, computed, watch, provide, nextTick, onMounted, onUnmounted } from 'vue'
import { useRouter, useRoute, RouterLink } from 'vue-router'
import type { AppLogDeviceSummary } from '@/types/device'
import type { AppLogEvent } from '@/types/applog'
import { api, ApiError } from '@/lib/api'
import { formatRelativeTime, lastSeenColorClass, formatNumber } from '@/lib/format'
import { LEVEL_RANK, levelColorClass, levelBgClass, levelBgColorClass } from '@/lib/applog-constants'
import { useAppLogDeviceLogs } from '@/composables/useAppLogDeviceLogs'
import ErrorDisplay from '@/components/ErrorDisplay.vue'
import LevelDistribution from '@/components/LevelDistribution.vue'
import AppLogRow from '@/components/AppLogRow.vue'

const props = defineProps<{
  hostname: string
}>()

const hostnameRef = computed(() => props.hostname)
const { events: deviceLogs } = useAppLogDeviceLogs(hostnameRef)

const router = useRouter()
const route = useRoute()
const initialTab = route.query.tab === 'recent' ? 'recent' : 'error'
const activeTab = ref<'error' | 'recent'>(initialTab)

// Persist tab selection in the URL query string.
watch(activeTab, (tab) => {
  router.replace({ query: { ...route.query, tab: tab === 'error' ? undefined : tab } })
})

const summary = ref<AppLogDeviceSummary | null>(null)
const loading = ref(true)
const error = ref<string | null>(null)
const errorStatus = ref<number | null>(null)

// Provide collapseSignal for AppLogRow expand/collapse on Escape.
const collapseSignal = ref(0)
provide('collapseSignal', collapseSignal)

// Recent logs reversed to chronological order (oldest first, newest at bottom).
const chronologicalLogs = computed(() => [...deviceLogs.value].reverse())

// Derive individual level counts from breakdown (matches dashboard pattern).
function lvlCount(level: string): number {
  return summary.value?.level_breakdown.find(l => l.level === level)?.count ?? 0
}
const fatalCount = computed(() => lvlCount('FATAL'))
const errorCount = computed(() => lvlCount('ERROR'))
const fatalErrorCount = computed(() => fatalCount.value + errorCount.value)
const warnCount = computed(() => lvlCount('WARN'))
const infoCount = computed(() => lvlCount('INFO'))

// Compute dynamic column widths for AppLogRow.
const colWidths = computed(() => {
  const events = activeTab.value === 'error'
    ? (summary.value?.error_logs ?? [])
    : chronologicalLogs.value
  const hostLen = props.hostname.length
  let maxSvc = 0
  let maxComp = 0
  for (const e of events) {
    if (e.service.length > maxSvc) maxSvc = e.service.length
    if (e.component.length > maxComp) maxComp = e.component.length
  }
  return {
    '--col-host': `${Math.min(20, Math.max(8, hostLen + 1))}ch`,
    '--col-svc': `${Math.min(16, Math.max(6, maxSvc + 1))}ch`,
    '--col-comp': `${Math.min(16, Math.max(6, maxComp + 1))}ch`,
    '--msg-lines': '3',
  }
})

// Auto-scroll the log container to bottom when pinned.
const logScrollEl = ref<HTMLElement | null>(null)
const isPinned = ref(true)

function scrollToBottom(behavior: ScrollBehavior = 'instant') {
  const el = logScrollEl.value
  if (!el) return
  el.scrollTo({ top: el.scrollHeight, behavior })
  isPinned.value = true
}

function onLogScroll() {
  const el = logScrollEl.value
  if (!el) return
  isPinned.value = el.scrollHeight - el.scrollTop - el.clientHeight < 30
}

// Auto-scroll to bottom when new events arrive (if pinned).
watch(chronologicalLogs, () => {
  if (isPinned.value) {
    nextTick(() => scrollToBottom())
  }
})

// Scroll to bottom on tab switch.
watch(activeTab, () => {
  isPinned.value = true
  nextTick(() => scrollToBottom())
})

function onKeydown(e: KeyboardEvent) {
  if (e.code !== 'Escape') return
  collapseSignal.value++
}

onMounted(() => {
  document.addEventListener('keydown', onKeydown)
})
onUnmounted(() => {
  document.removeEventListener('keydown', onKeydown)
})

async function fetchData() {
  try {
    const res = await api.getAppLogDeviceSummary(props.hostname)
    summary.value = res.data
    error.value = null
    errorStatus.value = null
  } catch (e) {
    if (!summary.value) {
      if (e instanceof ApiError) {
        errorStatus.value = e.status
        error.value = e.message
      } else {
        error.value = e instanceof Error ? e.message : 'failed to load device summary'
      }
    }
  } finally {
    loading.value = false
  }
}

// Update summary stats in real-time from SSE events.
let lastSeenEventId = 0
watch(deviceLogs, (logs) => {
  if (!summary.value || logs.length === 0) return
  for (const event of logs) {
    if (event.id <= lastSeenEventId) break
    summary.value.total_count++

    // Update last_seen_at.
    summary.value.last_seen_at = event.received_at

    // Update level breakdown counts + percentages.
    const existing = summary.value.level_breakdown.find(l => l.level === event.level)
    if (existing) {
      existing.count++
    } else {
      summary.value.level_breakdown.push({ level: event.level, count: 1, pct: 0 })
      summary.value.level_breakdown.sort(
        (a, b) => (LEVEL_RANK[a.level] ?? 99) - (LEVEL_RANK[b.level] ?? 99),
      )
    }
    // Recompute percentages.
    for (const l of summary.value.level_breakdown) {
      l.pct = (l.count / summary.value.total_count) * 100
    }

    // Error-level events (FATAL, ERROR).
    if (event.level === 'FATAL' || event.level === 'ERROR') {
      summary.value.error_count++
      summary.value.error_logs.unshift(event)
      if (summary.value.error_logs.length > 100) {
        summary.value.error_logs.splice(100)
      }
    }
  }
  lastSeenEventId = logs[0]!.id
}, { deep: false })

let refreshTimer: ReturnType<typeof setInterval> | undefined

watch(() => props.hostname, () => {
  clearInterval(refreshTimer)
  summary.value = null
  loading.value = true
  error.value = null
  errorStatus.value = null
  lastSeenEventId = 0
  fetchData()
  // Slow poll for top_messages accuracy and drift correction.
  refreshTimer = setInterval(fetchData, 30_000)
}, { immediate: true })

onUnmounted(() => {
  clearInterval(refreshTimer)
})

function currentEvents(): AppLogEvent[] {
  if (activeTab.value === 'error') return summary.value?.error_logs ?? []
  return chronologicalLogs.value
}
</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <!-- Loading state -->
    <div v-if="loading" class="text-t-fg-dark flex flex-1 items-center justify-center text-xs">loading...</div>

    <!-- Error states -->
    <div v-else-if="error" class="flex flex-1 items-center justify-center px-4 py-4">
      <ErrorDisplay
        v-if="errorStatus"
        :code="errorStatus"
        title="failed to load device"
        :message="error"
        :show-back="true"
        list-route="applog"
        list-label="go to applog"
      />
      <ErrorDisplay
        v-else
        title="nobody's home"
        message="the api isn't responding — it's probably down, restarting, or out getting coffee"
        :show-back="true"
        list-route="applog"
        list-label="go to applog"
      />
    </div>

    <!-- Main content -->
    <template v-else-if="summary">
      <!-- Summary area (compact, non-scrolling) -->
      <div class="shrink-0 space-y-4 p-4">
        <!-- Navigation -->
        <div class="flex items-center justify-between">
          <button
            class="text-t-teal text-xs hover:underline"
            @click="router.back()"
          >
            &larr; back
          </button>
          <RouterLink
            :to="{ name: 'applog', query: { host: summary.host } }"
            class="text-t-teal text-xs hover:underline"
          >
            view all logs &rarr;
          </RouterLink>
        </div>

        <!-- Summary stat cards -->
        <div class="grid grid-cols-2 gap-4 md:grid-cols-4">
          <!-- Hostname -->
          <div class="bg-t-bg-dark border-t-border rounded border p-4">
            <div class="text-t-fg-dark mb-1 text-xs uppercase tracking-wide">Host</div>
            <div class="text-t-teal truncate font-mono text-lg font-bold" :title="summary.host">{{ summary.host }}</div>
            <div class="mt-1 font-mono text-xs" :class="summary.last_seen_at ? lastSeenColorClass(summary.last_seen_at) : 'text-t-fg-dark'">
              {{ summary.last_seen_at ? formatRelativeTime(summary.last_seen_at) : 'never seen' }}
            </div>
          </div>

          <!-- Fatal & Errors -->
          <div class="bg-t-bg-dark border-t-border rounded border p-4">
            <div class="text-t-fg-dark mb-1 text-xs uppercase tracking-wide">Fatal & Errors</div>
            <div class="text-2xl font-bold">
              <RouterLink :to="{ name: 'applog', query: { host: summary.host, level_exact: 'FATAL' } }" class="text-sev-emerg hover:underline">{{ formatNumber(fatalCount) }}</RouterLink>
              <span class="text-t-fg-dark"> / </span>
              <RouterLink :to="{ name: 'applog', query: { host: summary.host, level_exact: 'ERROR' } }" class="text-sev-alert hover:underline">{{ formatNumber(errorCount) }}</RouterLink>
            </div>
            <div v-if="summary.total_count > 0" class="text-t-fg-dark mt-1 text-xs">
              {{ ((fatalErrorCount / summary.total_count) * 100).toFixed(1) }}% of total
            </div>
          </div>

          <!-- Warnings -->
          <div class="bg-t-bg-dark border-t-border rounded border p-4">
            <div class="text-t-fg-dark mb-1 text-xs uppercase tracking-wide">Warnings</div>
            <div class="text-2xl font-bold">
              <RouterLink :to="{ name: 'applog', query: { host: summary.host, level_exact: 'WARN' } }" class="text-sev-crit hover:underline">{{ formatNumber(warnCount) }}</RouterLink>
            </div>
            <div v-if="summary.total_count > 0" class="text-t-fg-dark mt-1 text-xs">
              {{ ((warnCount / summary.total_count) * 100).toFixed(1) }}% of total
            </div>
          </div>

          <!-- Info -->
          <div class="bg-t-bg-dark border-t-border rounded border p-4">
            <div class="text-t-fg-dark mb-1 text-xs uppercase tracking-wide">Info</div>
            <div class="text-2xl font-bold">
              <RouterLink :to="{ name: 'applog', query: { host: summary.host, level_exact: 'INFO' } }" class="text-sev-notice hover:underline">{{ formatNumber(infoCount) }}</RouterLink>
            </div>
            <div v-if="summary.total_count > 0" class="text-t-fg-dark mt-1 text-xs">
              {{ ((infoCount / summary.total_count) * 100).toFixed(1) }}% of total
            </div>
          </div>
        </div>

        <!-- Top Messages + Level Distribution -->
        <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <!-- Top Messages -->
          <div v-if="summary.top_messages.length > 0" class="bg-t-bg-dark border-t-border rounded border p-4">
            <h3 class="text-t-fg-dark mb-3 text-xs font-semibold uppercase tracking-wide">Top Messages <span class="text-t-fg-dark font-normal normal-case">(24h)</span></h3>
            <div class="-mx-4">
              <!-- Mobile: color bar + count + message -->
              <RouterLink
                v-for="(msg, i) in summary.top_messages.slice(0, 8)"
                :key="'m-' + i"
                :to="{ name: 'applog-detail', params: { id: msg.latest_id } }"
                class="hover:bg-t-bg-hover flex gap-2 py-1 pr-2 md:hidden"
                :class="levelBgClass[msg.level] ?? ''"
              >
                <div class="w-[3px] shrink-0 rounded-r" :class="levelBgColorClass[msg.level] ?? 'bg-sev-notice'" />
                <div class="min-w-0 flex-1">
                  <div class="text-t-purple text-[10px] leading-tight">{{ formatNumber(msg.count) }}x &middot; {{ msg.level }}</div>
                  <div class="min-w-0 truncate text-xs leading-snug">{{ msg.sample }}</div>
                </div>
              </RouterLink>
              <!-- Desktop: full row -->
              <RouterLink
                v-for="(msg, i) in summary.top_messages.slice(0, 8)"
                :key="'d-' + i"
                :to="{ name: 'applog-detail', params: { id: msg.latest_id } }"
                class="hover:bg-t-bg-hover hidden cursor-pointer items-baseline gap-3 px-4 py-px leading-snug transition-colors md:flex"
                :class="levelBgClass[msg.level] ?? ''"
              >
                <span class="text-t-fg-dark w-[10ch] shrink-0 text-right text-xs whitespace-nowrap">{{ formatRelativeTime(msg.latest_at) }}</span>
                <span class="text-t-purple w-[6ch] shrink-0 text-right text-xs">{{ formatNumber(msg.count) }}</span>
                <span class="w-[6ch] shrink-0 text-xs uppercase" :class="levelColorClass[msg.level] ?? 'text-t-fg'">{{ msg.level }}</span>
                <span class="min-w-0 flex-1 truncate" :title="msg.sample">{{ msg.sample }}</span>
              </RouterLink>
            </div>
          </div>

          <LevelDistribution
            v-if="summary.level_breakdown.length > 0"
            :items="summary.level_breakdown"
            title="Level Breakdown"
          />
        </div>
      </div>

      <!-- Logs section (fills remaining space) -->
      <div class="relative flex min-h-0 flex-1 flex-col">
        <!-- Tabs -->
        <div class="bg-t-bg-dark border-t-border mx-4 flex rounded-t border border-b-0">
          <button
            class="px-4 py-1.5 text-xs font-semibold uppercase tracking-wide transition-colors"
            :class="activeTab === 'error' ? 'text-t-teal' : 'text-t-fg-dark hover:text-t-fg'"
            @click="activeTab = 'error'"
          >
            Error Logs
          </button>
          <button
            class="px-4 py-1.5 text-xs font-semibold uppercase tracking-wide transition-colors"
            :class="activeTab === 'recent' ? 'text-t-teal' : 'text-t-fg-dark hover:text-t-fg'"
            @click="activeTab = 'recent'"
          >
            Recent Logs
          </button>
        </div>

        <!-- Log stream -->
        <div class="bg-t-bg-dark border-t-border mx-4 mb-4 flex min-h-0 flex-1 flex-col rounded-b border">
          <div
            v-if="currentEvents().length === 0"
            class="text-t-fg-dark flex flex-1 items-center justify-center text-xs"
          >
            {{ activeTab === 'error' ? 'no error events' : 'waiting for events...' }}
          </div>

          <div
            v-else
            ref="logScrollEl"
            role="log"
            aria-live="polite"
            :aria-label="activeTab === 'error' ? 'Error log events' : 'Live event stream'"
            class="flex-1 overflow-y-auto [overflow-anchor:none]"
            :style="colWidths"
            @scroll="onLogScroll"
          >
            <AppLogRow
              v-for="event in currentEvents()"
              :key="event.id"
              :event="event"
            />
          </div>
        </div>

        <!-- Scroll-to-bottom button when not pinned (recent tab only) -->
        <button
          v-if="activeTab === 'recent' && !isPinned && chronologicalLogs.length > 0"
          class="bg-t-bg-dark border-t-border absolute bottom-8 right-8 rounded border px-3 py-1.5 text-xs shadow-lg transition-colors hover:bg-t-bg-hover"
          @click="scrollToBottom('smooth')"
        >
          &darr; latest
        </button>
      </div>
    </template>
  </div>
</template>
