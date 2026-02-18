<script setup lang="ts">
import { ref, computed, watch, onUnmounted } from 'vue'
import { useRouter, RouterLink } from 'vue-router'
import type { AppLogDeviceSummary } from '@/types/device'
import { api, ApiError } from '@/lib/api'
import { formatRelativeTime, lastSeenColorClass, formatNumber } from '@/lib/format'
import { levelColorClass, levelBgClass } from '@/lib/applog-constants'
import { useAppLogDeviceLogs } from '@/composables/useAppLogDeviceLogs'
import ErrorDisplay from '@/components/ErrorDisplay.vue'
import LevelDistribution from '@/components/LevelDistribution.vue'
import RecentAppLogs from '@/components/RecentAppLogs.vue'

const props = defineProps<{
  hostname: string
}>()

const hostnameRef = computed(() => props.hostname)
const { events: deviceLogs } = useAppLogDeviceLogs(hostnameRef)
const activeTab = ref<'error' | 'all'>('error')

const router = useRouter()
const summary = ref<AppLogDeviceSummary | null>(null)
const loading = ref(true)
const error = ref<string | null>(null)
const errorStatus = ref<number | null>(null)

async function fetchData() {
  try {
    const res = await api.getAppLogDeviceSummary(props.hostname)
    summary.value = res.data
    error.value = null
    errorStatus.value = null
  } catch (e) {
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
        list-route="applog"
        list-label="go to applog"
      />
      <ErrorDisplay
        v-else-if="error"
        title="nobody's home"
        message="the api isn't responding — it's probably down, restarting, or out getting coffee"
        :show-back="false"
        list-route="applog"
        list-label="go to applog"
      />

      <div v-else-if="summary" class="mx-auto max-w-7xl space-y-4">
        <!-- Header -->
        <div class="bg-t-bg-dark border-t-border rounded border p-4">
          <h1 class="text-t-teal text-lg font-semibold font-mono">{{ summary.host }}</h1>
        </div>

        <!-- Overview + Level breakdown -->
        <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
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

              <dt class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-right">error logs</dt>
              <dd class="border-t-border border-b px-4 py-1.5 font-mono" :class="summary.error_count > 0 ? 'text-sev-err' : 'text-t-fg'">
                {{ formatNumber(summary.error_count) }}
              </dd>
            </dl>
          </div>

          <!-- Level breakdown -->
          <LevelDistribution v-if="summary.level_breakdown.length > 0" :items="summary.level_breakdown" title="Level Breakdown" />
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
              :to="{ name: 'applog-detail', params: { id: msg.latest_id } }"
              class="hover:bg-t-bg-hover flex cursor-pointer items-baseline gap-3 px-4 py-px leading-snug transition-colors"
              :class="levelBgClass[msg.level] ?? ''"
            >
              <span class="text-t-fg-dark w-[10ch] shrink-0 text-right text-xs whitespace-nowrap">{{ formatRelativeTime(msg.latest_at) }}</span>
              <span class="text-t-purple w-[8ch] shrink-0 text-right text-xs">{{ formatNumber(msg.count) }}</span>
              <span class="w-[6ch] shrink-0 uppercase" :class="levelColorClass[msg.level] ?? 'text-t-fg'">{{ msg.level }}</span>
              <span class="min-w-0 flex-1 truncate" :title="msg.sample">{{ msg.sample }}</span>
            </RouterLink>
          </div>
        </div>

        <!-- Logs tabs -->
        <div class="bg-t-bg-dark border-t-border rounded border">
          <div class="border-t-border flex border-b">
            <button
              class="px-4 py-1.5 text-xs font-semibold uppercase tracking-wide transition-colors"
              :class="activeTab === 'error' ? 'text-t-teal' : 'text-t-fg-dark hover:text-t-fg'"
              @click="activeTab = 'error'"
            >
              Error Logs
            </button>
            <button
              class="px-4 py-1.5 text-xs font-semibold uppercase tracking-wide transition-colors"
              :class="activeTab === 'all' ? 'text-t-teal' : 'text-t-fg-dark hover:text-t-fg'"
              @click="activeTab = 'all'"
            >
              Recent Logs
            </button>
          </div>

          <RecentAppLogs
            v-if="activeTab === 'error'"
            :events="summary.error_logs"
            highlight-level
            hide-header
          />
          <RecentAppLogs
            v-else
            :events="deviceLogs"
            highlight-level
            hide-header
          />
        </div>
      </div>
    </div>
  </div>
</template>
