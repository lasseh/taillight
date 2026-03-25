<script setup lang="ts">
import { ref, computed, watch, provide, nextTick, onMounted, onUnmounted } from 'vue'
import { useRouter, RouterLink } from 'vue-router'
import type { DeviceSummary } from '@/types/device'
import type { SyslogEvent } from '@/types/syslog'
import { api, ApiError } from '@/lib/api'
import { formatRelativeTime, lastSeenColorClass, formatNumber } from '@/lib/format'
import { severityColorClassByLabel, severityBgClass } from '@/lib/constants'
import { highlightMessage } from '@/lib/highlighter'
import { useDeviceLogs } from '@/composables/useDeviceLogs'
import ErrorDisplay from '@/components/ErrorDisplay.vue'
import SeverityDistribution from '@/components/SeverityDistribution.vue'
import SyslogRow from '@/components/SyslogRow.vue'

const props = defineProps<{
  hostname: string
}>()

const hostnameRef = computed(() => props.hostname)
const { events: deviceLogs } = useDeviceLogs(hostnameRef)
const activeTab = ref<'critical' | 'recent'>('critical')

const router = useRouter()
const summary = ref<DeviceSummary | null>(null)
const loading = ref(true)
const error = ref<string | null>(null)
const errorStatus = ref<number | null>(null)

// Provide collapseSignal for SyslogRow expand/collapse on Escape.
const collapseSignal = ref(0)
provide('collapseSignal', collapseSignal)

// Recent logs reversed to chronological order (oldest first, newest at bottom).
const chronologicalLogs = computed(() => [...deviceLogs.value].reverse())

// Compute dynamic column widths for SyslogRow.
const colWidths = computed(() => {
  const events = activeTab.value === 'critical'
    ? (summary.value?.critical_logs ?? [])
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
    const res = await api.getDeviceSummary(props.hostname)
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

let refreshTimer: ReturnType<typeof setInterval> | undefined

watch(() => props.hostname, () => {
  clearInterval(refreshTimer)
  summary.value = null
  loading.value = true
  error.value = null
  errorStatus.value = null
  fetchData()
  refreshTimer = setInterval(fetchData, 10_000)
}, { immediate: true })

onUnmounted(() => {
  clearInterval(refreshTimer)
})

function currentEvents(): SyslogEvent[] {
  if (activeTab.value === 'critical') return summary.value?.critical_logs ?? []
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
        list-route="syslog"
        list-label="go to syslog"
      />
      <ErrorDisplay
        v-else
        title="nobody's home"
        message="the api isn't responding — it's probably down, restarting, or out getting coffee"
        :show-back="true"
        list-route="syslog"
        list-label="go to syslog"
      />
    </div>

    <!-- Main content -->
    <template v-else-if="summary">
      <!-- Summary area (compact, non-scrolling) -->
      <div class="shrink-0 space-y-4 p-4">
        <!-- Back button + hostname -->
        <div class="flex items-center gap-3">
          <button
            class="text-t-fg-dark hover:text-t-fg text-xs transition-colors"
            @click="router.back()"
          >
            &larr; back
          </button>
          <h1 class="text-t-teal truncate font-mono text-lg font-semibold">{{ summary.hostname }}</h1>
        </div>

        <!-- Summary stat cards -->
        <div class="grid grid-cols-2 gap-4 md:grid-cols-4">
          <!-- Total -->
          <div class="bg-t-bg-dark border-t-border rounded border p-4">
            <div class="text-t-fg-dark mb-1 text-xs uppercase tracking-wide">Total (7d)</div>
            <div class="text-t-teal text-2xl font-bold">{{ formatNumber(summary.total_count) }}</div>
          </div>

          <!-- Critical -->
          <div class="bg-t-bg-dark border-t-border rounded border p-4">
            <div class="text-t-fg-dark mb-1 text-xs uppercase tracking-wide">Critical</div>
            <div class="text-2xl font-bold" :class="summary.critical_count > 0 ? 'text-sev-err' : 'text-t-fg'">
              {{ formatNumber(summary.critical_count) }}
            </div>
            <div v-if="summary.total_count > 0" class="text-t-fg-dark mt-1 text-xs">
              {{ ((summary.critical_count / summary.total_count) * 100).toFixed(1) }}% of total
            </div>
          </div>

          <!-- Last seen -->
          <div class="bg-t-bg-dark border-t-border rounded border p-4">
            <div class="text-t-fg-dark mb-1 text-xs uppercase tracking-wide">Last Log</div>
            <div class="text-2xl font-bold font-mono" :class="summary.last_seen_at ? lastSeenColorClass(summary.last_seen_at) : 'text-t-fg-dark'">
              {{ summary.last_seen_at ? formatRelativeTime(summary.last_seen_at) : 'never' }}
            </div>
          </div>

          <!-- Severity breakdown count (unique severities seen) -->
          <div class="bg-t-bg-dark border-t-border rounded border p-4">
            <div class="text-t-fg-dark mb-1 text-xs uppercase tracking-wide">Severities</div>
            <div class="text-t-purple text-2xl font-bold">{{ summary.severity_breakdown.length }}</div>
            <div class="text-t-fg-dark mt-1 text-xs">distinct levels</div>
          </div>
        </div>

        <!-- Top Messages + Severity Distribution -->
        <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <!-- Top Messages -->
          <div v-if="summary.top_messages.length > 0" class="bg-t-bg-dark border-t-border rounded border p-4">
            <h3 class="text-t-fg-dark mb-3 text-xs font-semibold uppercase tracking-wide">Top Messages</h3>
            <div class="-mx-4">
              <RouterLink
                v-for="(msg, i) in summary.top_messages.slice(0, 8)"
                :key="i"
                :to="{ name: 'syslog-detail', params: { id: msg.latest_id } }"
                class="hover:bg-t-bg-hover flex cursor-pointer items-baseline gap-3 px-4 py-px leading-snug transition-colors"
                :class="severityBgClass[msg.severity] ?? ''"
              >
                <span class="text-t-fg-dark w-[10ch] shrink-0 text-right text-xs whitespace-nowrap">{{ formatRelativeTime(msg.latest_at) }}</span>
                <span class="text-t-purple w-[6ch] shrink-0 text-right text-xs">{{ formatNumber(msg.count) }}</span>
                <span class="w-[6ch] shrink-0 text-xs uppercase" :class="severityColorClassByLabel[msg.severity_label] ?? 'text-t-fg'">{{ msg.severity_label }}</span>
                <span class="min-w-0 flex-1 truncate text-xs" :title="msg.sample" v-html="highlightMessage(msg.latest_id, msg.sample)" />
              </RouterLink>
            </div>
          </div>

          <SeverityDistribution
            v-if="summary.severity_breakdown.length > 0"
            :items="summary.severity_breakdown"
            title="Severity Breakdown"
          />
        </div>
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
            <SyslogRow
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
