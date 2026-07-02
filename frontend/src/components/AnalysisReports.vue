<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { api, ApiError } from '@/lib/api'
import { useAuthStore } from '@/stores/auth'
import { usePolling } from '@/composables/usePolling'
import {
  feedBadgeClass,
  feedDisplayLabel,
  formatDate,
  formatDuration,
  promptModeBadgeClass,
  reportTitle,
  statusBadgeClass,
  timeAgo,
} from '@/lib/analysis-format'
import type {
  AnalysisFeed,
  AnalysisHostEntry,
  AnalysisPromptMode,
  AnalysisReportListResponse,
  AnalysisReportSummary,
} from '@/types/analysis'

const auth = useAuthStore()
const isAdmin = computed(() => auth.user?.is_admin === true)

const reports = ref<AnalysisReportSummary[]>([])
const loadError = ref('')
const initialLoading = ref(true)

const showCreate = ref(false)
const selectedFeed = ref<AnalysisFeed>('netlog')
const selectedMode = ref<AnalysisPromptMode>('daily')
// Window in minutes used only when mode = incident. Daily/weekly use the
// server-side mode-aware default (24h / 7d) so the period selector is hidden.
const incidentPeriodMinutes = ref(60)
const creating = ref(false)
const createError = ref('')

const confirmedFeeds: { value: AnalysisFeed; label: string }[] = [
  { value: 'netlog', label: 'Netlog' },
  { value: 'srvlog', label: 'Srvlog' },
  { value: 'all', label: 'All syslog' },
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

// Host picker state. selectedHosts is empty = "all hosts on the feed",
// matching the server's canonical {} representation. hostQuery is what the
// user is typing into the autocomplete input. availableHosts is the list
// loaded from /api/v1/analysis/hosts for the currently-selected feed; it
// re-fetches whenever selectedFeed changes.
const selectedHosts = ref<string[]>([])
const hostQuery = ref('')
const hostsLoading = ref(false)
const hostsError = ref('')
const availableHosts = ref<AnalysisHostEntry[]>([])
const highlightedIndex = ref(0)
// Names the server rejected on the last create attempt — used to badge bad
// chips so the user can see exactly which entries failed validation.
const unknownHostNames = ref<Set<string>>(new Set())

// Suggestions = available hosts not already selected, filtered by the
// current query (case-insensitive substring). Stable order = the server's
// alphabetical order, so the highlighted index lines up predictably with
// what the user sees.
const hostSuggestions = computed<AnalysisHostEntry[]>(() => {
  const q = hostQuery.value.trim().toLowerCase()
  const taken = new Set(selectedHosts.value)
  return availableHosts.value.filter((h) => {
    if (taken.has(h.hostname)) return false
    if (q === '') return true
    return h.hostname.toLowerCase().includes(q)
  })
})

// Reset highlight whenever the suggestion set changes so arrow-key
// navigation always starts from the top of the new visible list.
watch(hostSuggestions, () => {
  highlightedIndex.value = 0
})

async function loadHostsForFeed(feed: AnalysisFeed) {
  hostsLoading.value = true
  hostsError.value = ''
  try {
    const res = await api.listAnalysisHosts(feed)
    availableHosts.value = res.data
  } catch (e) {
    availableHosts.value = []
    hostsError.value = e instanceof Error ? e.message : 'failed to load hosts'
  } finally {
    hostsLoading.value = false
  }
}

// Refetch the host list every time the feed changes. If the user has
// already picked hosts, warn before dropping any that don't exist in the
// new feed — silent removal is the kind of "lost my work" surprise the
// confirmation exists to prevent.
watch(selectedFeed, async (next, prev) => {
  if (next === prev) return
  await loadHostsForFeed(next)
  if (selectedHosts.value.length === 0) return
  const valid = new Set(availableHosts.value.map((h) => h.hostname))
  const stale = selectedHosts.value.filter((h) => !valid.has(h))
  if (stale.length === 0) return
  const ok = window.confirm(
    `${stale.length} selected host${stale.length === 1 ? '' : 's'} ` +
      `don't exist on the ${next} feed and will be removed: ${stale.join(', ')}. Continue?`,
  )
  if (ok) {
    selectedHosts.value = selectedHosts.value.filter((h) => valid.has(h))
  } else {
    // User cancelled — revert to the previous feed so chips stay valid.
    selectedFeed.value = prev
  }
})

function addHost(name: string) {
  const trimmed = name.trim()
  if (trimmed === '') return
  if (selectedHosts.value.includes(trimmed)) return
  selectedHosts.value = [...selectedHosts.value, trimmed]
  hostQuery.value = ''
  unknownHostNames.value.delete(trimmed)
}

function removeHost(name: string) {
  selectedHosts.value = selectedHosts.value.filter((h) => h !== name)
  unknownHostNames.value.delete(name)
}

function clearHosts() {
  selectedHosts.value = []
  unknownHostNames.value = new Set()
}

function addAllMatching() {
  for (const h of hostSuggestions.value) {
    if (!selectedHosts.value.includes(h.hostname)) {
      selectedHosts.value.push(h.hostname)
    }
  }
  hostQuery.value = ''
}

function onHostKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter') {
    e.preventDefault()
    const pick = hostSuggestions.value[highlightedIndex.value]
    if (pick) addHost(pick.hostname)
    return
  }
  if (e.key === 'Backspace' && hostQuery.value === '' && selectedHosts.value.length > 0) {
    // Pop the last chip on backspace-in-empty so the user can edit the
    // list without reaching for the mouse.
    e.preventDefault()
    const last = selectedHosts.value[selectedHosts.value.length - 1]
    if (last) removeHost(last)
    return
  }
  if (e.key === 'ArrowDown') {
    e.preventDefault()
    highlightedIndex.value = Math.min(highlightedIndex.value + 1, hostSuggestions.value.length - 1)
    return
  }
  if (e.key === 'ArrowUp') {
    e.preventDefault()
    highlightedIndex.value = Math.max(highlightedIndex.value - 1, 0)
    return
  }
}

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

function cancelCreate() {
  showCreate.value = false
  createError.value = ''
}

async function createReport() {
  createError.value = ''
  creating.value = true
  unknownHostNames.value = new Set()
  try {
    const payload: {
      feed: AnalysisFeed
      prompt_mode: AnalysisPromptMode
      period_minutes?: number
      hosts?: string[]
    } = {
      feed: selectedFeed.value,
      prompt_mode: selectedMode.value,
    }
    if (selectedMode.value === 'incident') {
      payload.period_minutes = incidentPeriodMinutes.value
    }
    if (selectedHosts.value.length > 0) {
      payload.hosts = selectedHosts.value
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
      } else if (e.code === 'unknown_hosts') {
        // Server returns the bad names inline in the message. Parse the
        // brackets so the picker can badge each offender; keep the full
        // message as the error text for callers who want detail.
        createError.value = e.message
        const match = e.message.match(/\[([^\]]+)\]/)
        const captured = match?.[1]
        if (captured) {
          const bad = captured
            .split(/\s+/)
            .map((s) => s.trim())
            .filter(Boolean)
          unknownHostNames.value = new Set(bad)
        }
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

onMounted(async () => {
  await refresh()
  await loadHostsForFeed(selectedFeed.value)
})
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
                  class="flex items-center gap-2 rounded border px-3 py-1.5 text-sm transition-all"
                  :class="
                    selectedFeed === opt.value
                      ? 'bg-t-orange/15 border-t-orange text-t-orange'
                      : 'border-t-border text-t-fg-dark hover:text-t-fg hover:border-t-fg-dark'
                  "
                  @click="selectedFeed = opt.value"
                >
                  <span class="w-4 text-center text-xs">{{
                    selectedFeed === opt.value ? '✓' : ''
                  }}</span>
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
                  <span class="w-4 text-center text-xs">{{
                    selectedMode === opt.value ? '✓' : ''
                  }}</span>
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

            <label class="block">
              <span class="text-t-fg-dark text-sm">hosts</span>
              <div class="border-t-border bg-t-bg mt-1.5 rounded border p-2">
                <div class="flex flex-wrap items-center gap-1.5">
                  <span
                    v-for="h in selectedHosts"
                    :key="h"
                    class="inline-flex items-center gap-1 rounded px-2 py-0.5 text-xs"
                    :class="
                      unknownHostNames.has(h)
                        ? 'bg-t-red/15 text-t-red border border-t-red/40'
                        : 'bg-t-orange/15 text-t-orange'
                    "
                  >
                    {{ h }}
                    <button
                      type="button"
                      class="hover:brightness-150"
                      :title="`remove ${h}`"
                      @click="removeHost(h)"
                    >
                      ×
                    </button>
                  </span>
                  <input
                    v-model="hostQuery"
                    type="text"
                    :placeholder="
                      selectedHosts.length === 0 ? 'All hosts. Type to filter…' : 'add another…'
                    "
                    class="text-t-fg placeholder:text-t-fg-gutter min-w-[8rem] flex-1 bg-transparent text-sm outline-none"
                    @keydown="onHostKeydown"
                  />
                </div>
              </div>
              <div class="mt-1 flex items-center justify-between text-xs">
                <span class="text-t-fg-gutter">
                  <span v-if="hostsLoading">loading hosts…</span>
                  <span v-else-if="hostsError" class="text-t-red">{{ hostsError }}</span>
                  <span v-else-if="selectedHosts.length === 0">
                    leave empty to analyze every host on the feed
                  </span>
                  <span v-else>
                    {{ selectedHosts.length }} host{{ selectedHosts.length === 1 ? '' : 's' }}
                    selected
                  </span>
                </span>
                <span class="flex items-center gap-3">
                  <button
                    v-if="hostQuery.trim() !== '' && hostSuggestions.length > 1"
                    type="button"
                    class="text-t-orange hover:brightness-125"
                    @click="addAllMatching"
                  >
                    add all {{ hostSuggestions.length }} matching
                  </button>
                  <button
                    v-if="selectedHosts.length > 0"
                    type="button"
                    class="text-t-fg-dark hover:text-t-fg"
                    @click="clearHosts"
                  >
                    clear all
                  </button>
                </span>
              </div>
              <div
                v-if="hostQuery.trim() !== '' && hostSuggestions.length > 0"
                class="border-t-border bg-t-bg-dark mt-1 max-h-48 overflow-y-auto rounded border"
              >
                <button
                  v-for="(h, i) in hostSuggestions"
                  :key="h.hostname"
                  type="button"
                  class="w-full px-2 py-1 text-left text-sm transition-colors"
                  :class="
                    i === highlightedIndex
                      ? 'bg-t-bg-highlight text-t-fg'
                      : 'text-t-fg-dark hover:text-t-fg hover:bg-t-bg-hover'
                  "
                  @mouseenter="highlightedIndex = i"
                  @click="addHost(h.hostname)"
                >
                  {{ h.hostname }}
                </button>
              </div>
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
                @click="cancelCreate"
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

        <div
          v-if="reports.length > 0"
          class="text-t-fg-gutter border-t-border flex items-center border-b px-5 py-2 text-xs uppercase tracking-wider"
        >
          <span class="min-w-0 flex-1">Report</span>
          <span class="w-20 shrink-0">Source</span>
          <span class="w-20 shrink-0">Mode</span>
          <span class="w-28 shrink-0">Status</span>
          <span class="w-24 shrink-0 text-right">Created</span>
          <span class="w-20 shrink-0 text-right">Duration</span>
        </div>

        <div class="divide-t-border divide-y">
          <div
            v-for="r in reports"
            :key="r.id"
            class="hover:bg-t-bg-hover flex items-center px-5 py-3 text-sm transition-colors"
          >
            <div class="min-w-0 flex-1 pr-4 flex items-center gap-2">
              <router-link
                :to="{ name: 'analysis-report', params: { slug: r.slug } }"
                class="text-t-fg hover:text-t-orange font-medium transition-colors"
              >
                {{ reportTitle(r) }}
              </router-link>
              <span
                class="bg-t-fg-dark/10 text-t-fg-dark inline-block rounded px-1.5 py-0.5 font-mono text-xs"
              >
                {{ formatDate(r.created_at) }}
              </span>
            </div>
            <div class="w-20 shrink-0">
              <span
                class="inline-block rounded px-1.5 py-0.5 text-xs"
                :class="feedBadgeClass(r.feed)"
              >
                {{ feedDisplayLabel(r.feed) }}
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
            <div class="w-28 shrink-0">
              <span
                class="inline-block rounded px-1.5 py-0.5 text-xs"
                :class="statusBadgeClass(r.status)"
              >
                {{ r.status }}
              </span>
            </div>
            <div class="w-24 shrink-0 text-right">
              <span
                class="bg-t-fg-dark/10 text-t-fg-dark inline-block rounded px-1.5 py-0.5 text-xs"
                :title="formatDate(r.created_at)"
              >
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
  transition:
    opacity 0.15s ease,
    transform 0.15s ease;
}

.slide-enter-from,
.slide-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}
</style>
