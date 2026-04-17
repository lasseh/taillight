<script setup lang="ts">
import { ref, computed, watch, provide, nextTick, onMounted, onUnmounted } from 'vue'
import { useRouter, useRoute, RouterLink } from 'vue-router'
import type { DeviceSummary } from '@/types/device'
import type { NetlogEvent } from '@/types/netlog'
import { api, ApiError } from '@/lib/api'
import { formatRelativeTime, lastSeenColorClass, formatNumber } from '@/lib/format'
import { severityLabels, severityColorClassByLabel, severityBgClass, severityBgClassByLabel } from '@/lib/constants'
import { highlightMessage } from '@/lib/highlighter'
import { useNetlogDeviceLogs } from '@/composables/useNetlogDeviceLogs'
import { useDeviceSummaryCollapsed } from '@/composables/useDeviceSummaryCollapsed'
import ErrorDisplay from '@/components/ErrorDisplay.vue'
import SeverityDistribution from '@/components/SeverityDistribution.vue'
import NetlogRow from '@/components/NetlogRow.vue'

const props = defineProps<{
  hostname: string
}>()

const hostnameRef = computed(() => props.hostname)
const { events: deviceLogs } = useNetlogDeviceLogs(hostnameRef)

const summaryCollapsed = useDeviceSummaryCollapsed()

const router = useRouter()
const route = useRoute()
const initialTab = route.query.tab === 'recent' ? 'recent' : 'critical'
const activeTab = ref<'critical' | 'recent'>(initialTab)

// Persist tab selection in the URL query string.
watch(activeTab, (tab) => {
  router.replace({ query: { ...route.query, tab: tab === 'critical' ? undefined : tab } })
})
const summary = ref<DeviceSummary | null>(null)
const loading = ref(true)
const error = ref<string | null>(null)
const errorStatus = ref<number | null>(null)

// Provide collapseSignal for NetlogRow expand/collapse on Escape.
const collapseSignal = ref(0)
provide('collapseSignal', collapseSignal)

// Recent logs reversed to chronological order (oldest first, newest at bottom).
const chronologicalLogs = computed(() => [...deviceLogs.value].reverse())

// Critical logs reversed to match chronological order (newest at bottom).
const chronologicalCriticalLogs = computed(() => [...(summary.value?.critical_logs ?? [])].reverse())

// Derive individual severity counts from breakdown (matches dashboard pattern).
function sevCount(severity: number): number {
  return summary.value?.severity_breakdown.find(s => s.severity === severity)?.count ?? 0
}
const emergCount = computed(() => sevCount(0))
const alertCount = computed(() => sevCount(1))
const emergAlertCount = computed(() => emergCount.value + alertCount.value)
const critCount = computed(() => sevCount(2))
const errCount = computed(() => sevCount(3))

// Compute dynamic column widths for NetlogRow.
const colWidths = computed(() => {
  const events = activeTab.value === 'critical'
    ? chronologicalCriticalLogs.value
    : chronologicalLogs.value
  const hostLen = props.hostname.length
  let maxProg = 0
  for (const e of events) {
    if (e.programname.length > maxProg) maxProg = e.programname.length
  }
  return {
    '--col-host': `${Math.min(20, Math.max(8, hostLen + 1))}ch`,
    '--col-prog': `${Math.min(16, Math.max(6, maxProg + 1))}ch`,
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
    const res = await api.getNetlogDeviceSummary(props.hostname)
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
  // deviceLogs is newest-first; only process events we haven't seen.
  for (const event of logs) {
    if (event.id <= lastSeenEventId) break
    summary.value.total_count++

    // Update last_seen_at.
    summary.value.last_seen_at = event.received_at

    // Update severity breakdown counts + percentages.
    const label = severityLabels[event.severity] ?? 'unknown'
    const existing = summary.value.severity_breakdown.find(s => s.severity === event.severity)
    if (existing) {
      existing.count++
    } else {
      summary.value.severity_breakdown.push({ severity: event.severity, label, count: 1, pct: 0 })
      summary.value.severity_breakdown.sort((a, b) => a.severity - b.severity)
    }
    // Recompute percentages.
    for (const s of summary.value.severity_breakdown) {
      s.pct = (s.count / summary.value.total_count) * 100
    }

    // Critical events (emerg=0, alert=1, crit=2).
    if (event.severity <= 2) {
      summary.value.critical_count++
      summary.value.critical_logs.unshift(event)
      if (summary.value.critical_logs.length > 100) {
        summary.value.critical_logs.splice(100)
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

function currentEvents(): NetlogEvent[] {
  if (activeTab.value === 'critical') return chronologicalCriticalLogs.value
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
        list-route="netlog"
        list-label="go to netlog"
      />
      <ErrorDisplay
        v-else
        title="nobody's home"
        message="the api isn't responding — it's probably down, restarting, or out getting coffee"
        :show-back="true"
        list-route="netlog"
        list-label="go to netlog"
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
            :to="{ name: 'netlog', query: { hostname: summary.hostname } }"
            class="text-t-teal text-xs hover:underline"
          >
            view all logs &rarr;
          </RouterLink>
        </div>

        <!-- Summary stat cards -->
        <div class="grid grid-cols-2 gap-4 md:grid-cols-4">
          <!-- Hostname -->
          <div class="bg-t-bg-dark border-t-border rounded border p-4">
            <div class="text-t-fg-dark mb-1 text-xs uppercase tracking-wide">Hostname</div>
            <div class="text-t-teal truncate font-mono text-lg font-bold" :title="summary.hostname">{{ summary.hostname }}</div>
            <div v-if="summary.fromhost_ip" class="text-t-fg-dark font-mono text-xs">{{ summary.fromhost_ip }}</div>
            <div class="mt-1 font-mono text-xs" :class="summary.last_seen_at ? lastSeenColorClass(summary.last_seen_at) : 'text-t-fg-dark'">
              {{ summary.last_seen_at ? formatRelativeTime(summary.last_seen_at) : 'never seen' }}
            </div>
          </div>

          <!-- Emerg & Alert -->
          <div class="bg-t-bg-dark border-t-border rounded border p-4">
            <div class="text-t-fg-dark mb-1 text-xs uppercase tracking-wide">Emerg & Alert</div>
            <div class="text-2xl font-bold">
              <RouterLink :to="{ name: 'netlog', query: { hostname: summary.hostname, severity: '0' } }" class="text-sev-emerg hover:underline">{{ formatNumber(emergCount) }}</RouterLink>
              <span class="text-t-fg-dark"> / </span>
              <RouterLink :to="{ name: 'netlog', query: { hostname: summary.hostname, severity: '1' } }" class="text-sev-alert hover:underline">{{ formatNumber(alertCount) }}</RouterLink>
            </div>
            <div v-if="summary.total_count > 0" class="text-t-fg-dark mt-1 text-xs">
              {{ ((emergAlertCount / summary.total_count) * 100).toFixed(1) }}% of total
            </div>
          </div>

          <!-- Criticals -->
          <div class="bg-t-bg-dark border-t-border rounded border p-4">
            <div class="text-t-fg-dark mb-1 text-xs uppercase tracking-wide">Criticals</div>
            <div class="text-2xl font-bold">
              <RouterLink :to="{ name: 'netlog', query: { hostname: summary.hostname, severity: '2' } }" class="text-sev-crit hover:underline">{{ formatNumber(critCount) }}</RouterLink>
            </div>
            <div v-if="summary.total_count > 0" class="text-t-fg-dark mt-1 text-xs">
              {{ ((critCount / summary.total_count) * 100).toFixed(1) }}% of total
            </div>
          </div>

          <!-- Errors -->
          <div class="bg-t-bg-dark border-t-border rounded border p-4">
            <div class="text-t-fg-dark mb-1 text-xs uppercase tracking-wide">Errors</div>
            <div class="text-2xl font-bold">
              <RouterLink :to="{ name: 'netlog', query: { hostname: summary.hostname, severity: '3' } }" class="text-sev-err hover:underline">{{ formatNumber(errCount) }}</RouterLink>
            </div>
            <div v-if="summary.total_count > 0" class="text-t-fg-dark mt-1 text-xs">
              {{ ((errCount / summary.total_count) * 100).toFixed(1) }}% of total
            </div>
          </div>
        </div>

        <!-- Top Messages + Severity Distribution -->
        <div v-if="!summaryCollapsed" class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <!-- Top Messages -->
          <div v-if="summary.top_messages.length > 0" class="bg-t-bg-dark border-t-border rounded border p-4">
            <h3 class="text-t-fg-dark mb-3 text-xs font-semibold uppercase tracking-wide">Top Messages <span class="text-t-fg-dark font-normal normal-case">(24h)</span></h3>
            <div class="-mx-4">
              <!-- Mobile: color bar + count + message -->
              <RouterLink
                v-for="(msg, i) in summary.top_messages.slice(0, 8)"
                :key="'m-' + i"
                :to="{ name: 'netlog-detail', params: { id: msg.latest_id } }"
                class="hover:bg-t-bg-hover flex gap-2 py-1 pr-2 md:hidden"
                :class="severityBgClass[msg.severity] ?? ''"
              >
                <div class="w-[3px] shrink-0 rounded-r" :class="severityBgClassByLabel[msg.severity_label] ?? 'bg-sev-info'" />
                <div class="min-w-0 flex-1">
                  <div class="text-t-purple text-[10px] leading-tight">{{ formatNumber(msg.count) }}x &middot; {{ msg.severity_label }}</div>
                  <div class="min-w-0 truncate text-xs leading-snug" v-html="highlightMessage(msg.latest_id, msg.sample)" />
                </div>
              </RouterLink>
              <!-- Desktop: full row -->
              <RouterLink
                v-for="(msg, i) in summary.top_messages.slice(0, 8)"
                :key="'d-' + i"
                :to="{ name: 'netlog-detail', params: { id: msg.latest_id } }"
                class="hover:bg-t-bg-hover hidden cursor-pointer items-baseline gap-3 px-4 py-px leading-snug transition-colors md:flex"
                :class="severityBgClass[msg.severity] ?? ''"
              >
                <span class="text-t-fg-dark w-[10ch] shrink-0 text-right text-xs whitespace-nowrap">{{ formatRelativeTime(msg.latest_at) }}</span>
                <span class="text-t-purple w-[6ch] shrink-0 text-right text-xs">{{ formatNumber(msg.count) }}</span>
                <span class="w-[6ch] shrink-0 text-xs uppercase" :class="severityColorClassByLabel[msg.severity_label] ?? 'text-t-fg'">{{ msg.severity_label }}</span>
                <span class="min-w-0 flex-1 truncate" :title="msg.sample" v-html="highlightMessage(msg.latest_id, msg.sample)" />
              </RouterLink>
            </div>
          </div>

          <SeverityDistribution
            v-if="summary.severity_breakdown.length > 0"
            :items="summary.severity_breakdown"
            title="Severity Breakdown"
            collapsible
            @collapse="summaryCollapsed = true"
          />
        </div>
        <button
          v-else
          type="button"
          class="bg-t-bg-dark border-t-border hover:bg-t-bg-hover text-t-fg-dark hover:text-t-fg flex w-full items-center justify-center rounded border py-1.5 transition-colors"
          aria-label="Expand summary"
          @click="summaryCollapsed = false"
        >
          <svg class="h-4 w-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="6 9 12 15 18 9" /></svg>
        </button>
      </div>

      <!-- Logs section (fills remaining space) -->
      <div class="relative flex min-h-0 flex-1 flex-col">
        <!-- Tabs -->
        <div class="bg-t-bg-dark border-t-border mx-4 flex rounded-t border border-b-0">
          <button
            class="px-4 py-1.5 text-xs font-semibold uppercase tracking-wide transition-colors"
            :class="activeTab === 'critical' ? 'text-t-teal' : 'text-t-fg-dark hover:text-t-fg'"
            @click="activeTab = 'critical'"
          >
            Critical Logs
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
            {{ activeTab === 'critical' ? 'no critical events' : 'waiting for events...' }}
          </div>

          <div
            v-else
            ref="logScrollEl"
            role="log"
            aria-live="polite"
            :aria-label="activeTab === 'critical' ? 'Critical log events' : 'Live event stream'"
            class="flex-1 overflow-y-auto [overflow-anchor:none]"
            :style="colWidths"
            @scroll="onLogScroll"
          >
            <NetlogRow
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
