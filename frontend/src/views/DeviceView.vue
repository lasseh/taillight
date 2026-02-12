<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import type { DeviceSummary } from '@/types/device'
import { api, ApiError } from '@/lib/api'
import { formatRelativeTime, lastSeenColorClass, formatNumber } from '@/lib/format'
import { severityColorClassByLabel, severityBgClassByLabel } from '@/lib/constants'
import ErrorDisplay from '@/components/ErrorDisplay.vue'

const props = defineProps<{
  hostname: string
}>()

const router = useRouter()
const summary = ref<DeviceSummary | null>(null)
const loading = ref(true)
const error = ref<string | null>(null)
const errorStatus = ref<number | null>(null)

onMounted(async () => {
  try {
    const res = await api.getDeviceSummary(props.hostname)
    summary.value = res.data
  } catch (e) {
    if (e instanceof ApiError && e.code !== 'unknown') {
      errorStatus.value = e.status
      error.value = e.message
    } else {
      error.value = e instanceof Error ? e.message : 'failed to load device summary'
    }
  } finally {
    loading.value = false
  }
})

const sevTotal = computed(() => {
  if (!summary.value) return 0
  return summary.value.severity_breakdown.reduce((sum, s) => sum + s.count, 0)
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

      <div v-else-if="summary" class="mx-auto max-w-4xl space-y-4">
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
            <dt class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-right">last seen</dt>
            <dd class="border-t-border border-b px-4 py-1.5 font-mono" :class="summary.last_seen_at ? lastSeenColorClass(summary.last_seen_at) : 'text-t-fg-dark'">
              {{ summary.last_seen_at ? formatRelativeTime(summary.last_seen_at) : 'never' }}
            </dd>

            <dt class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-right">critical logs</dt>
            <dd class="border-t-border border-b px-4 py-1.5 font-mono" :class="summary.critical_count > 0 ? 'text-sev-err' : 'text-t-fg'">
              {{ formatNumber(summary.critical_count) }}
            </dd>
          </dl>
        </div>

        <!-- Severity breakdown -->
        <div v-if="summary.severity_breakdown.length > 0" class="bg-t-bg-dark border-t-border rounded border">
          <h3 class="text-t-fg-dark border-t-border border-b px-4 py-2 text-xs font-semibold uppercase tracking-wide">
            Severity Breakdown
          </h3>
          <div class="space-y-2 p-4">
            <div
              v-for="sev in summary.severity_breakdown"
              :key="sev.severity"
              class="flex items-center gap-3 text-sm"
            >
              <span class="w-16 shrink-0 text-right uppercase" :class="severityColorClassByLabel[sev.label] ?? 'text-t-fg'">
                {{ sev.label }}
              </span>
              <div class="bg-t-bg h-4 min-w-0 flex-1 rounded">
                <div
                  class="h-4 rounded"
                  :class="severityBgClassByLabel[sev.label] ?? 'bg-t-fg-dark'"
                  :style="{ width: sevTotal > 0 ? `${(sev.count / sevTotal) * 100}%` : '0%' }"
                />
              </div>
              <span class="text-t-fg-dark w-16 shrink-0 text-right font-mono text-xs">
                {{ formatNumber(sev.count) }}
              </span>
              <span class="text-t-fg-dark w-12 shrink-0 text-right font-mono text-xs">
                {{ sev.pct.toFixed(1) }}%
              </span>
            </div>
          </div>
        </div>

        <!-- Top messages -->
        <div v-if="summary.top_messages.length > 0" class="bg-t-bg-dark border-t-border rounded border">
          <h3 class="text-t-fg-dark border-t-border border-b px-4 py-2 text-xs font-semibold uppercase tracking-wide">
            Top Messages
          </h3>
          <div class="divide-t-border divide-y">
            <div
              v-for="(msg, i) in summary.top_messages"
              :key="i"
              class="flex items-baseline gap-3 px-4 py-1.5 text-sm"
            >
              <span class="text-t-fg-dark w-16 shrink-0 text-right font-mono text-xs">
                {{ formatNumber(msg.count) }}
              </span>
              <span class="text-t-fg min-w-0 flex-1 truncate font-mono text-xs" :title="msg.sample">
                {{ msg.sample }}
              </span>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
