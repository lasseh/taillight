<script setup lang="ts">
import { ref, computed, watch, provide, nextTick, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
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
      <!-- Header bar -->
      <div class="border-t-border flex shrink-0 items-center gap-3 border-b px-4 py-2">
        <button
          class="text-t-fg-dark hover:text-t-fg text-xs transition-colors"
          @click="router.back()"
        >
          &larr; back
        </button>
        <h1 class="text-t-teal truncate font-mono text-sm font-semibold">{{ summary.hostname }}</h1>
        <span class="text-t-fg-dark ml-auto text-xs font-mono" :class="summary.last_seen_at ? lastSeenColorClass(summary.last_seen_at) : ''">
          {{ summary.last_seen_at ? formatRelativeTime(summary.last_seen_at) : 'never seen' }}
        </span>
      </div>

      <!-- Top section: Overview + Severity side-by-side -->
      <div class="border-t-border shrink-0 border-b">
        <div class="grid grid-cols-1 md:grid-cols-2">
          <!-- Overview -->
          <div class="border-t-border md:border-r">
            <h3 class="text-t-teal border-t-border border-b px-4 py-1.5 text-xs font-semibold uppercase tracking-wide">
              Overview (7 days)
            </h3>
            <dl class="grid grid-cols-[auto_1fr] text-sm">
              <dt class="text-t-fg-dark border-t-border border-b px-4 py-1 text-right text-xs">total logs</dt>
              <dd class="text-t-teal border-t-border border-b px-4 py-1 font-mono text-xs">
                {{ formatNumber(summary.total_count) }}
              </dd>
              <dt class="text-t-fg-dark border-t-border border-b px-4 py-1 text-right text-xs">critical</dt>
              <dd class="border-t-border border-b px-4 py-1 font-mono text-xs" :class="summary.critical_count > 0 ? 'text-sev-err' : 'text-t-fg'">
                {{ formatNumber(summary.critical_count) }}
              </dd>
            </dl>
          </div>

          <!-- Severity Breakdown -->
          <div>
            <SeverityDistribution
              v-if="summary.severity_breakdown.length > 0"
              :items="summary.severity_breakdown"
              title="Severity Breakdown"
            />
            <div v-else class="text-t-fg-dark flex items-center justify-center py-6 text-xs">no severity data</div>
          </div>
        </div>
      </div>

      <!-- Top Messages (compact) -->
      <div v-if="summary.top_messages.length > 0" class="border-t-border shrink-0 border-b">
        <h3 class="text-t-teal border-t-border border-b px-4 py-1.5 text-xs font-semibold uppercase tracking-wide">
          Top Messages
        </h3>
        <div>
          <RouterLink
            v-for="(msg, i) in summary.top_messages.slice(0, 8)"
            :key="i"
            :to="{ name: 'syslog-detail', params: { id: msg.latest_id } }"
            class="hover:bg-t-bg-hover flex cursor-pointer items-baseline gap-3 px-4 py-px leading-snug transition-colors"
            :class="severityBgClass[msg.severity] ?? ''"
          >
            <span class="text-t-fg-dark w-[10ch] shrink-0 text-right text-xs whitespace-nowrap">{{ formatRelativeTime(msg.latest_at) }}</span>
            <span class="text-t-purple w-[8ch] shrink-0 text-right text-xs">{{ formatNumber(msg.count) }}</span>
            <span class="w-[8ch] shrink-0 uppercase" :class="severityColorClassByLabel[msg.severity_label] ?? 'text-t-fg'">{{ msg.severity_label }}</span>
            <span class="min-w-0 flex-1 truncate" :title="msg.sample" v-html="highlightMessage(msg.latest_id, msg.sample)" />
          </RouterLink>
        </div>
      </div>

      <!-- Tabs -->
      <div class="border-t-border flex shrink-0 border-b">
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

      <!-- Log stream (fills remaining space) -->
      <div class="relative flex min-h-0 flex-1 flex-col">
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

        <!-- Scroll-to-bottom button when not pinned (recent tab only) -->
        <button
          v-if="activeTab === 'recent' && !isPinned && chronologicalLogs.length > 0"
          class="bg-t-bg-dark border-t-border absolute bottom-4 right-4 rounded border px-3 py-1.5 text-xs shadow-lg transition-colors hover:bg-t-bg-hover"
          @click="scrollToBottom('smooth')"
        >
          &darr; latest
        </button>
      </div>
    </template>
  </div>
</template>
