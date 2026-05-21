<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { api, ApiError } from '@/lib/api'
import { features as getFeatures } from '@/lib/features'
import { useAuthStore } from '@/stores/auth'
import { usePolling } from '@/composables/usePolling'
import {
  feedBadgeClass,
  formatDate,
  formatDuration,
  promptModeBadgeClass,
  statusBadgeClass,
  timeAgo,
} from '@/lib/analysis-format'
import type {
  AnalysisFeed,
  AnalysisPromptMode,
  AnalysisReportListResponse,
  AnalysisReportSummary,
} from '@/types/analysis'

const auth = useAuthStore()
const features = getFeatures()
const isAdmin = computed(() => auth.user?.is_admin === true)

const reports = ref<AnalysisReportSummary[]>([])
const loadError = ref('')
const initialLoading = ref(true)

const showCreate = ref(false)
const selectedFeed = ref<AnalysisFeed>(features.netlog ? 'netlog' : 'srvlog')
const selectedMode = ref<AnalysisPromptMode>('daily')
// Window in minutes used only when mode = incident. Daily/weekly use the
// server-side mode-aware default (24h / 7d) so the period selector is hidden.
const incidentPeriodMinutes = ref(60)
const creating = ref(false)
const createError = ref('')

const confirmedFeeds: { value: AnalysisFeed; label: string; available: boolean }[] = [
  { value: 'netlog', label: 'Netlog', available: features.netlog },
  { value: 'srvlog', label: 'Srvlog', available: true },
  { value: 'all', label: 'All', available: features.netlog },
]

const promptModes: { value: AnalysisPromptMode; label: string; hint: string }[] = [
  { value: 'daily', label: 'Daily', hint: 'last 24h, ops brief framing' },
  { value: 'weekly', label: 'Weekly', hint: 'last 7d, trend review framing' },
  { value: 'incident', label: 'Incident', hint: 'narrow window, live triage' },
]

const incidentPeriodOptions: { minutes: number; label: string }[] = [
  { minutes: 15, label: '15 min' },
  { minutes: 30, label: '30 min' },
  { minutes: 60, label: '1 hour' },
  { minutes: 180, label: '3 hours' },
]

// Reports tab uses smart polling: keep ticking while any row is still pending
// or running, and stop once everything is terminal so the page goes idle.
const polling = usePolling<AnalysisReportListResponse>(
  () => api.listAnalysisReports(),
  (res) => res.data.some((r) => r.status === 'pending' || r.status === 'running'),
  3000,
)

// initialLoading clears the "loading..." placeholder once the poller's first
// tick lands. polling.start() does the initial fetch, so we don't double-fetch.
async function refresh() {
  try {
    await polling.start()
    if (polling.error.value) {
      loadError.value = errorMessage(polling.error.value)
    } else {
      loadError.value = ''
    }
  } finally {
    initialLoading.value = false
  }
}

function errorMessage(e: unknown): string {
  if (e instanceof ApiError) return e.message
  if (e instanceof Error) return e.message
  return 'failed to load reports'
}

watch(
  () => polling.data.value,
  (res) => {
    if (res) reports.value = res.data
  },
)

async function createReport() {
  createError.value = ''
  creating.value = true
  try {
    const payload: { feed: AnalysisFeed; prompt_mode: AnalysisPromptMode; period_minutes?: number } = {
      feed: selectedFeed.value,
      prompt_mode: selectedMode.value,
    }
    if (selectedMode.value === 'incident') {
      payload.period_minutes = incidentPeriodMinutes.value
    }
    const res = await api.createAnalysisReport(payload)
    reports.value = [res.data, ...reports.value]
    showCreate.value = false
    void polling.start()
  } catch (e) {
    if (e instanceof ApiError) {
      if (e.code === 'duplicate_report') {
        createError.value = 'a report for this feed and mode is already pending or running'
      } else if (e.code === 'queue_full') {
        createError.value = 'analysis queue is full — try again shortly'
      } else if (e.code === 'feed_unavailable') {
        createError.value = 'this feed is disabled on the server'
      } else if (e.code === 'invalid_prompt_mode' || e.code === 'invalid_period') {
        createError.value = e.message
      } else {
        createError.value = e.message
      }
    } else {
      createError.value = e instanceof Error ? e.message : 'failed to start analysis'
    }
  } finally {
    creating.value = false
  }
}

onMounted(refresh)
</script>

<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between">
      <p class="text-t-fg-dark text-sm">analysis reports for log activity</p>
      <button
        v-if="isAdmin && !showCreate"
        class="bg-t-bg-highlight text-t-fg hover:brightness-125 border-t-border border px-4 py-2 text-sm transition-all"
        @click="showCreate = true"
      >
        + generate report
      </button>
    </div>

    <div v-if="initialLoading" class="text-t-fg-dark py-10 text-center text-sm">loading...</div>
    <div v-else-if="loadError" class="text-t-red py-10 text-center text-sm">{{ loadError }}</div>

    <template v-else>
      <Transition name="slide">
        <div v-if="isAdmin && showCreate" class="bg-t-bg-dark border-t-border rounded border p-5">
          <h3 class="text-t-fg mb-4 text-sm font-semibold">Generate a new analysis report</h3>
          <div class="space-y-4">
            <label class="block">
              <span class="text-t-fg-dark text-sm">source</span>
              <div class="mt-1.5 flex flex-wrap gap-2">
                <button
                  v-for="opt in confirmedFeeds"
                  :key="opt.value"
                  :disabled="!opt.available"
                  class="flex items-center gap-2 rounded border px-3 py-1.5 text-sm transition-all"
                  :class="
                    !opt.available
                      ? 'border-t-border text-t-fg-gutter cursor-not-allowed opacity-60'
                      : selectedFeed === opt.value
                        ? 'bg-t-orange/15 border-t-orange text-t-orange'
                        : 'border-t-border text-t-fg-dark hover:text-t-fg hover:border-t-fg-dark'
                  "
                  @click="opt.available && (selectedFeed = opt.value)"
                >
                  <span class="w-4 text-center text-xs">{{ selectedFeed === opt.value ? '✓' : '' }}</span>
                  {{ opt.label }}
                </button>
                <button
                  disabled
                  class="border-t-border text-t-fg-gutter flex items-center gap-2 rounded border px-3 py-1.5 text-sm opacity-60 cursor-not-allowed"
                  title="applog analyzer coming soon"
                >
                  <span class="w-4 text-center text-xs"></span>
                  Applog
                  <span class="text-t-fg-gutter text-xs">(soon)</span>
                </button>
              </div>
              <p class="text-t-fg-gutter mt-2 text-xs">
                for recurring runs on a longer cadence, set up a schedule.
              </p>
            </label>

            <label class="block">
              <span class="text-t-fg-dark text-sm">framing</span>
              <div class="mt-1.5 flex flex-wrap gap-2">
                <button
                  v-for="opt in promptModes"
                  :key="opt.value"
                  class="flex items-center gap-2 rounded border px-3 py-1.5 text-sm transition-all"
                  :class="
                    selectedMode === opt.value
                      ? 'bg-t-orange/15 border-t-orange text-t-orange'
                      : 'border-t-border text-t-fg-dark hover:text-t-fg hover:border-t-fg-dark'
                  "
                  :title="opt.hint"
                  @click="selectedMode = opt.value"
                >
                  <span class="w-4 text-center text-xs">{{ selectedMode === opt.value ? '✓' : '' }}</span>
                  {{ opt.label }}
                </button>
              </div>
              <p class="text-t-fg-gutter mt-2 text-xs">
                {{ promptModes.find((m) => m.value === selectedMode)?.hint }}
              </p>
            </label>

            <label v-if="selectedMode === 'incident'" class="block">
              <span class="text-t-fg-dark text-sm">window</span>
              <div class="mt-1.5 flex flex-wrap gap-2">
                <button
                  v-for="opt in incidentPeriodOptions"
                  :key="opt.minutes"
                  class="flex items-center gap-2 rounded border px-3 py-1.5 text-sm transition-all"
                  :class="
                    incidentPeriodMinutes === opt.minutes
                      ? 'bg-t-orange/15 border-t-orange text-t-orange'
                      : 'border-t-border text-t-fg-dark hover:text-t-fg hover:border-t-fg-dark'
                  "
                  @click="incidentPeriodMinutes = opt.minutes"
                >
                  <span class="w-4 text-center text-xs">{{
                    incidentPeriodMinutes === opt.minutes ? '✓' : ''
                  }}</span>
                  {{ opt.label }}
                </button>
              </div>
              <p class="text-t-fg-gutter mt-2 text-xs">
                how far back to scan for the active spike.
              </p>
            </label>

            <div class="flex items-center gap-3 pt-1">
              <button
                class="bg-t-bg-highlight text-t-fg hover:brightness-125 border-t-border border px-4 py-2 text-sm transition-all"
                :disabled="creating"
                @click="createReport"
              >
                {{ creating ? 'queuing...' : 'generate report' }}
              </button>
              <button
                class="text-t-fg-dark hover:text-t-fg text-sm transition-colors"
                @click="showCreate = false; createError = ''"
              >
                cancel
              </button>
              <span v-if="createError" class="text-t-red text-sm">{{ createError }}</span>
            </div>
          </div>
        </div>
      </Transition>

      <div class="bg-t-bg-dark border-t-border rounded border">
        <div class="border-t-border border-b px-5 py-2.5">
          <h3 class="text-t-fg-dark text-xs font-semibold uppercase tracking-wide">
            Reports
            <span class="text-t-fg-gutter ml-1 font-normal normal-case">{{ reports.length }}</span>
          </h3>
        </div>

        <div v-if="reports.length > 0" class="text-t-fg-gutter border-t-border flex border-b px-5 py-2 text-xs uppercase tracking-wider">
          <span class="min-w-0 flex-1">Name</span>
          <span class="w-20 shrink-0">Source</span>
          <span class="w-20 shrink-0">Mode</span>
          <span class="w-44 shrink-0">Status</span>
          <span class="w-24 shrink-0">Created</span>
          <span class="w-20 shrink-0 text-right">Duration</span>
        </div>

        <div class="divide-t-border divide-y">
          <div
            v-for="r in reports"
            :key="r.id"
            class="hover:bg-t-bg-hover flex items-center px-5 py-3 text-sm transition-colors"
          >
            <div class="min-w-0 flex-1">
              <router-link
                :to="{ name: 'analysis-report', params: { slug: r.slug } }"
                class="text-t-fg hover:text-t-orange font-mono font-medium transition-colors"
              >
                {{ r.slug }}
              </router-link>
            </div>
            <div class="w-20 shrink-0">
              <span
                class="inline-block rounded px-1.5 py-0.5 text-xs"
                :class="feedBadgeClass(r.feed)"
              >
                {{ r.feed }}
              </span>
            </div>
            <div class="w-20 shrink-0">
              <span
                class="inline-block rounded px-1.5 py-0.5 text-xs"
                :class="promptModeBadgeClass(r.prompt_mode)"
              >
                {{ r.prompt_mode }}
              </span>
            </div>
            <div class="w-44 shrink-0">
              <span
                class="inline-block rounded px-1.5 py-0.5 text-xs"
                :class="statusBadgeClass(r.status)"
              >
                {{ r.status }}
              </span>
              <span v-if="r.status === 'pending'" class="text-t-fg-gutter ml-2 text-xs">
                queued, will start shortly
              </span>
            </div>
            <div class="w-24 shrink-0">
              <span class="text-t-fg-dark text-xs" :title="formatDate(r.created_at)">
                {{ timeAgo(r.created_at) }}
              </span>
            </div>
            <div class="w-20 shrink-0 text-right">
              <span class="text-t-fg-dark text-xs">{{ formatDuration(r) || '—' }}</span>
            </div>
          </div>
        </div>

        <div v-if="reports.length === 0" class="px-5 py-10 text-center">
          <p class="text-t-fg-dark text-sm">no analysis reports yet</p>
          <button
            v-if="isAdmin && !showCreate"
            class="text-t-orange mt-2 text-sm hover:brightness-125"
            @click="showCreate = true"
          >
            generate your first report
          </button>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.slide-enter-active,
.slide-leave-active {
  transition: opacity 0.15s ease, transform 0.15s ease;
}

.slide-enter-from,
.slide-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}
</style>
