<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRouter, RouterLink } from 'vue-router'
import type { DeviceSummary } from '@/types/device'
import { api, ApiError } from '@/lib/api'
import { formatRelativeTime, lastSeenColorClass, formatNumber } from '@/lib/format'
import { severityColorClassByLabel, severityBgClass } from '@/lib/constants'
import { highlightMessage } from '@/lib/highlighter'
import { useDeviceLogs } from '@/composables/useDeviceLogs'
import ErrorDisplay from '@/components/ErrorDisplay.vue'
import SeverityDistribution from '@/components/SeverityDistribution.vue'
import RecentCriticalLogs from '@/components/RecentCriticalLogs.vue'

const props = defineProps<{
  hostname: string
}>()

const hostnameRef = computed(() => props.hostname)
const { events: deviceLogs } = useDeviceLogs(hostnameRef)
const activeTab = ref<'critical' | 'all'>('critical')

const router = useRouter()
const summary = ref<DeviceSummary | null>(null)
const loading = ref(true)
const error = ref<string | null>(null)
const errorStatus = ref<number | null>(null)

async function fetchData() {
  try {
    const res = await api.getDeviceSummary(props.hostname)
    summary.value = res.data
    // Clear any previous error on successful refresh.
    error.value = null
    errorStatus.value = null
  } catch (e) {
    // Only set error state on initial load; silently ignore refresh failures.
    if (!summary.value) {
      if (e instanceof ApiError && e.code !== 'unknown') {
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

onMounted(() => {
  fetchData()
  refreshTimer = setInterval(fetchData, 10_000)
})

onUnmounted(() => {
  clearInterval(refreshTimer)
})

</script>

<template>
  <div class="flex min-h-0 flex-1 flex-col">
    <div class="flex-1 overflow-y-auto px-4 py-4">
      <button
        class="text-t-fg-dark hover:text-t-fg mb-4 text-xs transition-colors"
        @click="router.back()"
      >
        &larr; back
      </button>

      <div v-if="loading" class="text-t-fg-dark text-xs">loading...</div>

      <ErrorDisplay
        v-else-if="error && errorStatus"
        :code="errorStatus"
        title="failed to load device"
        :message="error"
        :show-back="false"
        list-route="syslog"
        list-label="go to syslog"
      />
      <ErrorDisplay
        v-else-if="error"
        title="nobody's home"
        message="the api isn't responding — it's probably down, restarting, or out getting coffee"
        :show-back="false"
        list-route="syslog"
        list-label="go to syslog"
      />

      <div v-else-if="summary" class="mx-auto max-w-7xl space-y-4">
        <!-- Header -->
        <div class="bg-t-bg-dark border-t-border rounded border p-4">
          <h1 class="text-t-teal text-lg font-semibold font-mono">{{ summary.hostname }}</h1>
        </div>

        <!-- Info panel -->
        <div class="bg-t-bg-dark border-t-border rounded border">
          <h3 class="text-t-fg-dark border-t-border border-b px-4 py-2 text-xs font-semibold uppercase tracking-wide">
            Overview (7 days)
          </h3>
          <dl class="grid grid-cols-[auto_1fr] text-sm">
            <dt class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-right">last log</dt>
            <dd class="border-t-border border-b px-4 py-1.5 font-mono" :class="summary.last_seen_at ? lastSeenColorClass(summary.last_seen_at) : 'text-t-fg-dark'">
              {{ summary.last_seen_at ? formatRelativeTime(summary.last_seen_at) : 'never' }}
            </dd>

            <dt class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-right">total logs</dt>
            <dd class="text-t-teal border-t-border border-b px-4 py-1.5 font-mono">
              {{ formatNumber(summary.total_count) }}
            </dd>

            <dt class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-right">critical logs</dt>
            <dd class="border-t-border border-b px-4 py-1.5 font-mono" :class="summary.critical_count > 0 ? 'text-sev-err' : 'text-t-fg'">
              {{ formatNumber(summary.critical_count) }}
            </dd>
          </dl>
        </div>

        <!-- Top messages -->
        <div v-if="summary.top_messages.length > 0" class="bg-t-bg-dark border-t-border rounded border">
          <h3 class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-xs font-semibold uppercase tracking-wide">
            Top Messages
          </h3>
          <div>
            <RouterLink
              v-for="(msg, i) in summary.top_messages"
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

        <!-- Severity breakdown -->
        <SeverityDistribution v-if="summary.severity_breakdown.length > 0" :items="summary.severity_breakdown" title="Severity Breakdown" />

        <!-- Logs tabs -->
        <div class="bg-t-bg-dark border-t-border rounded border">
          <div class="border-t-border flex border-b">
            <button
              class="px-4 py-1.5 text-xs font-semibold uppercase tracking-wide transition-colors"
              :class="activeTab === 'critical' ? 'text-t-teal' : 'text-t-fg-dark hover:text-t-fg'"
              @click="activeTab = 'critical'"
            >
              Critical Logs
            </button>
            <button
              class="px-4 py-1.5 text-xs font-semibold uppercase tracking-wide transition-colors"
              :class="activeTab === 'all' ? 'text-t-teal' : 'text-t-fg-dark hover:text-t-fg'"
              @click="activeTab = 'all'"
            >
              Recent Logs
            </button>
          </div>

          <RecentCriticalLogs
            v-if="activeTab === 'critical'"
            :events="summary.critical_logs"
            highlight-severity
            hide-header
          />
          <RecentCriticalLogs
            v-else
            :events="deviceLogs"
            highlight-severity
            hide-header
          />
        </div>
      </div>
    </div>
  </div>
</template>
