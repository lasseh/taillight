<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed, watch } from 'vue'
import { useElementSize } from '@vueuse/core'
import { useHomeStore } from '@/stores/home'
import { useTheme } from '@/composables/useTheme'
import { useDashboardLayout } from '@/composables/useDashboardLayout'
import { useNewFlash } from '@/composables/useNewFlash'
import { formatTime, formatAttrs, formatNumber } from '@/lib/format'
import { severityColorClassByLabel, severityBgClassByLabel } from '@/lib/constants'
import { LEVEL_RANK, levelColorClass, levelBgColorClass } from '@/lib/applog-constants'
import SeverityDistribution from '@/components/SeverityDistribution.vue'
import RecentCriticalLogs from '@/components/RecentCriticalLogs.vue'
import ActivityHeatmap from '@/components/ActivityHeatmap.vue'

defineOptions({ name: 'HomeView' })

const home = useHomeStore()
const { current: theme } = useTheme()
const { editing, isVisible, hideWidget, stopEditing, resetLayout, allHidden } = useDashboardLayout()
const accentColors = computed(() => theme.value.chartColors)

const anySyslogWidgetVisible = computed(() =>
  ['syslog-summary', 'syslog-distribution', 'syslog-recent'].some(id => isVisible(id)),
)
const anyApplogWidgetVisible = computed(() =>
  ['applog-summary', 'applog-distribution', 'applog-recent'].some(id => isVisible(id)),
)
const anyHeatmapVisible = computed(() =>
  ['syslog-heatmap', 'applog-heatmap'].some(id => isVisible(id)),
)

const rangePresets = [
  { label: '1h', value: '1h' },
  { label: '6h', value: '6h' },
  { label: '24h', value: '24h' },
  { label: '7d', value: '7d' },
  { label: '30d', value: '30d' },
]

const rangeLabel = computed(() => {
  const p = rangePresets.find(p => p.value === home.range)
  return p ? p.label : home.range
})

// Syslog: extract individual severity counts from severity_breakdown
const syslogEmerg = computed(() => {
  if (!home.syslogSummary) return 0
  return (home.syslogSummary.severity_breakdown ?? [])
    .find(s => s.severity === 0)?.count ?? 0
})

const syslogAlert = computed(() => {
  if (!home.syslogSummary) return 0
  return (home.syslogSummary.severity_breakdown ?? [])
    .find(s => s.severity === 1)?.count ?? 0
})

const syslogEmergAlert = computed(() => syslogEmerg.value + syslogAlert.value)

const syslogCriticals = computed(() => {
  if (!home.syslogSummary) return 0
  return (home.syslogSummary.severity_breakdown ?? [])
    .find(s => s.severity === 2)?.count ?? 0
})

const syslogErrors = computed(() => {
  if (!home.syslogSummary) return 0
  return (home.syslogSummary.severity_breakdown ?? [])
    .find(s => s.severity === 3)?.count ?? 0
})

// Applog: level distribution sorted by severity (highest first)
const sortedLevelBreakdown = computed(() => {
  if (!home.applogSummary) return []
  return [...(home.applogSummary.level_breakdown ?? [])].sort(
    (a, b) => (LEVEL_RANK[a.level] ?? 99) - (LEVEL_RANK[b.level] ?? 99),
  )
})

// Applog: extract fatal count from level_breakdown
const applogFatal = computed(() => {
  if (!home.applogSummary) return 0
  return (home.applogSummary.level_breakdown ?? [])
    .find(l => l.level === 'FATAL')?.count ?? 0
})

const applogErrors = computed(() => {
  if (!home.applogSummary) return 0
  return (home.applogSummary.level_breakdown ?? [])
    .find(l => l.level === 'ERROR')?.count ?? 0
})

const applogFatalErrors = computed(() => applogFatal.value + applogErrors.value)

// Applog: extract info count from level_breakdown
const applogInfo = computed(() => {
  if (!home.applogSummary) return 0
  return (home.applogSummary.level_breakdown ?? [])
    .find(l => l.level === 'INFO')?.count ?? 0
})

const hasSyslogData = computed(() => home.syslogSummary && home.syslogSummary.total > 0)
const hasApplogData = computed(() => home.applogSummary && home.applogSummary.total > 0)

// Dynamic list sizing: measure the list container and compute how many items fit
const ITEM_HEIGHT = 24 // each row ~24px (text-xs + space-y-2)

const hostsListEl = ref<HTMLElement | null>(null)
const servicesListEl = ref<HTMLElement | null>(null)
const { height: hostsListHeight } = useElementSize(hostsListEl)
const { height: servicesListHeight } = useElementSize(servicesListEl)

const visibleHosts = computed(() => {
  if (!home.syslogSummary) return []
  const count = Math.max(5, Math.floor(hostsListHeight.value / ITEM_HEIGHT))
  return (home.syslogSummary.top_hosts ?? []).slice(0, count)
})

const visibleServices = computed(() => {
  if (!home.applogSummary) return []
  const count = Math.max(5, Math.floor(servicesListHeight.value / ITEM_HEIGHT))
  return (home.applogSummary.top_services ?? []).slice(0, count)
})

// Track new event IDs for flash highlight
const { ids: newSyslogIds, reset: resetSyslogFlash } = useNewFlash(() => home.recentSyslogEvents)
const { ids: newApplogIds, reset: resetApplogFlash } = useNewFlash(() => home.recentApplogEvents)

// Suppress flash when switching time range (data swap, not new events)
watch(() => home.range, () => {
  resetSyslogFlash()
  resetApplogFlash()
})

onMounted(() => {
  home.startRefresh()
})

onUnmounted(() => {
  home.stopRefresh()
  if (editing.value) stopEditing()
})

// ═══════════════════════════════════════════════════════════════════════════
// Helpers
// ═══════════════════════════════════════════════════════════════════════════

function getSeverityColorClass(level: string): string {
  return levelColorClass[level] ?? severityColorClassByLabel[level.toLowerCase()] ?? 'text-t-fg'
}

function getSeverityBgClass(level: string): string {
  return levelBgColorClass[level] ?? severityBgClassByLabel[level.toLowerCase()] ?? 'bg-t-fg'
}

</script>

<template>
  <div class="flex flex-1 flex-col gap-8 overflow-y-auto p-4">
    <!-- Loading state (first load only) -->
    <div v-if="!home.loaded" class="text-t-fg-dark flex flex-1 items-center justify-center text-sm">
      Loading dashboard...
    </div>

    <template v-else>
      <!-- Error banner -->
      <div v-if="home.error" class="text-t-red bg-t-red/10 border-t-red/30 rounded border px-4 py-2 text-sm">
        {{ home.error }}
      </div>

      <!-- All widgets hidden -->
      <div v-if="allHidden && !editing" class="text-t-fg-dark flex flex-1 flex-col items-center justify-center gap-2 text-sm">
        <span>All dashboard widgets are hidden.</span>
        <button class="text-t-teal hover:underline" @click="resetLayout()">Reset layout</button>
      </div>

      <!-- ═══════════════════════════ SYSLOG SECTION ═══════════════════════════ -->
      <section v-if="anySyslogWidgetVisible || editing">
        <h2 class="text-t-teal mb-4 flex items-center gap-2 text-sm font-semibold uppercase tracking-wider">
          <RouterLink to="/syslog" class="bg-t-teal/20 rounded px-2 py-0.5 hover:bg-t-teal/30 transition-colors">Syslog</RouterLink>
          <span class="bg-t-border h-px flex-1"></span>
          <span class="flex items-center gap-1">
            <button
              v-for="p in rangePresets"
              :key="p.value"
              class="rounded px-1.5 py-0.5 text-xs transition-colors"
              :class="home.range === p.value ? 'bg-t-bg-highlight text-t-purple' : 'text-t-fg-dark hover:text-t-fg'"
              @click="home.setRange(p.value)"
            >{{ p.label }}</button>
          </span>
        </h2>

        <!-- Empty state -->
        <div v-if="!hasSyslogData" class="bg-t-bg-dark border-t-border text-t-fg-dark rounded border px-6 py-10 text-center text-sm">
          No syslog events in the last {{ rangeLabel }}
        </div>

        <template v-else>
          <!-- Summary Cards -->
          <div v-if="isVisible('syslog-summary')" class="relative mb-4">
            <button v-if="editing" class="absolute -right-1.5 -top-1.5 z-10 flex h-5 w-5 items-center justify-center rounded-full border border-t-border bg-t-bg-dark text-t-fg-dark transition-colors hover:border-t-red hover:text-t-red" @click="hideWidget('syslog-summary')"><svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" /></svg></button>
            <div class="grid grid-cols-2 gap-4 md:grid-cols-4">
              <!-- Total -->
              <div class="bg-t-bg-dark border-t-border rounded border p-4">
                <div class="text-t-fg-dark mb-1 text-xs uppercase tracking-wide">Total {{ rangeLabel }}</div>
                <div class="text-t-teal text-2xl font-bold">{{ formatNumber(home.syslogSummary!.total) }}</div>
                <div class="mt-1 flex items-center gap-1 text-xs">
                  <span :class="home.syslogSummary!.trend >= 0 ? 'text-t-green' : 'text-t-red'">
                    {{ home.syslogSummary!.trend >= 0 ? '&#x25B2;' : '&#x25BC;' }}{{ Math.abs(home.syslogSummary!.trend).toFixed(1) }}%
                  </span>
                  <span class="text-t-fg-dark">vs prev {{ rangeLabel }}</span>
                </div>
              </div>

              <!-- Emerg & Alert -->
              <div class="bg-t-bg-dark border-t-border rounded border p-4">
                <div class="text-t-fg-dark mb-1 text-xs uppercase tracking-wide">Emerg & Alert</div>
                <div class="text-2xl font-bold"><RouterLink :to="{ name: 'syslog', query: { severity: '0' } }" class="text-sev-emerg hover:underline">{{ formatNumber(syslogEmerg) }}</RouterLink> <span class="text-t-fg-dark">/</span> <RouterLink :to="{ name: 'syslog', query: { severity: '1' } }" class="text-sev-alert hover:underline">{{ formatNumber(syslogAlert) }}</RouterLink></div>
                <div class="text-t-fg-dark mt-1 text-xs">
                  {{ home.syslogSummary!.total > 0 ? ((syslogEmergAlert / home.syslogSummary!.total) * 100).toFixed(1) : 0 }}% of total
                </div>
              </div>

              <!-- Criticals -->
              <div class="bg-t-bg-dark border-t-border rounded border p-4">
                <div class="text-t-fg-dark mb-1 text-xs uppercase tracking-wide">Criticals</div>
                <div class="text-2xl font-bold"><RouterLink :to="{ name: 'syslog', query: { severity: '2' } }" class="text-sev-crit hover:underline">{{ formatNumber(syslogCriticals) }}</RouterLink></div>
                <div class="text-t-fg-dark mt-1 text-xs">
                  {{ home.syslogSummary!.total > 0 ? ((syslogCriticals / home.syslogSummary!.total) * 100).toFixed(1) : 0 }}% of total
                </div>
              </div>

              <!-- Errors -->
              <div class="bg-t-bg-dark border-t-border rounded border p-4">
                <div class="text-t-fg-dark mb-1 text-xs uppercase tracking-wide">Errors</div>
                <div class="text-2xl font-bold"><RouterLink :to="{ name: 'syslog', query: { severity: '3' } }" class="text-sev-err hover:underline">{{ formatNumber(syslogErrors) }}</RouterLink></div>
                <div class="text-t-fg-dark mt-1 text-xs">
                  {{ home.syslogSummary!.total > 0 ? ((syslogErrors / home.syslogSummary!.total) * 100).toFixed(1) : 0 }}% of total
                </div>
              </div>
            </div>
          </div>

          <!-- Two Column Layout -->
          <div v-if="isVisible('syslog-distribution')" class="relative mb-4">
            <button v-if="editing" class="absolute -right-1.5 -top-1.5 z-10 flex h-5 w-5 items-center justify-center rounded-full border border-t-border bg-t-bg-dark text-t-fg-dark transition-colors hover:border-t-red hover:text-t-red" @click="hideWidget('syslog-distribution')"><svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" /></svg></button>
            <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
              <!-- Severity Distribution -->
              <SeverityDistribution :items="home.syslogSummary!.severity_breakdown" />

              <!-- Top Hosts -->
              <div class="bg-t-bg-dark border-t-border flex flex-col rounded border p-4">
                <h3 class="text-t-fg-dark mb-3 text-xs font-semibold uppercase tracking-wide">Top Hosts</h3>
                <div ref="hostsListEl" class="min-h-[120px] flex-1 space-y-2 overflow-hidden">
                  <RouterLink
                    v-for="(host, idx) in visibleHosts"
                    :key="host.name"
                    :to="{ name: 'device-detail', params: { hostname: host.name } }"
                    class="group flex cursor-pointer items-center gap-2"
                  >
                    <span class="text-t-fg-dark w-4 text-xs">{{ idx + 1 }}.</span>
                    <span class="w-28 truncate text-sm md:w-40" :style="{ color: accentColors[idx % accentColors.length] }">{{ host.name }}</span>
                    <div class="bg-t-bg-highlight h-2 flex-1 overflow-hidden rounded">
                      <div
                        class="h-full rounded transition-all group-hover:opacity-80"
                        :style="{ width: `${host.pct}%`, opacity: 0.6, backgroundColor: accentColors[idx % accentColors.length] }"
                      ></div>
                    </div>
                    <span class="text-t-fg-dark w-8 text-right text-xs">{{ host.pct.toFixed(0) }}%</span>
                    <span class="text-t-fg w-10 text-right text-xs">{{ formatNumber(host.count) }}</span>
                  </RouterLink>
                </div>
              </div>
            </div>
          </div>

          <!-- Recent High-Severity Events -->
          <div v-if="isVisible('syslog-recent')" class="relative">
            <button v-if="editing" class="absolute -right-1.5 -top-1.5 z-10 flex h-5 w-5 items-center justify-center rounded-full border border-t-border bg-t-bg-dark text-t-fg-dark transition-colors hover:border-t-red hover:text-t-red" @click="hideWidget('syslog-recent')"><svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" /></svg></button>
            <RecentCriticalLogs :events="home.recentSyslogEvents" show-hostname :flash-ids="newSyslogIds" />
          </div>
        </template>
      </section>

      <!-- ═══════════════════════════ APPLOG SECTION ═══════════════════════════ -->
      <section v-if="anyApplogWidgetVisible || editing">
        <h2 class="text-t-magenta mb-4 flex items-center gap-2 text-sm font-semibold uppercase tracking-wider">
          <RouterLink to="/applog" class="bg-t-magenta/20 rounded px-2 py-0.5 hover:bg-t-magenta/30 transition-colors">Applog</RouterLink>
          <span class="bg-t-border h-px flex-1"></span>
          <span class="flex items-center gap-1">
            <button
              v-for="p in rangePresets"
              :key="p.value"
              class="rounded px-1.5 py-0.5 text-xs transition-colors"
              :class="home.range === p.value ? 'bg-t-bg-highlight text-t-purple' : 'text-t-fg-dark hover:text-t-fg'"
              @click="home.setRange(p.value)"
            >{{ p.label }}</button>
          </span>
        </h2>

        <!-- Empty state -->
        <div v-if="!hasApplogData" class="bg-t-bg-dark border-t-border text-t-fg-dark rounded border px-6 py-10 text-center text-sm">
          No application log events in the last {{ rangeLabel }}
        </div>

        <template v-else>
          <!-- Summary Cards -->
          <div v-if="isVisible('applog-summary')" class="relative mb-4">
            <button v-if="editing" class="absolute -right-1.5 -top-1.5 z-10 flex h-5 w-5 items-center justify-center rounded-full border border-t-border bg-t-bg-dark text-t-fg-dark transition-colors hover:border-t-red hover:text-t-red" @click="hideWidget('applog-summary')"><svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" /></svg></button>
            <div class="grid grid-cols-2 gap-4 md:grid-cols-4">
              <!-- Total -->
              <div class="bg-t-bg-dark border-t-border rounded border p-4">
                <div class="text-t-fg-dark mb-1 text-xs uppercase tracking-wide">Total {{ rangeLabel }}</div>
                <div class="text-t-teal text-2xl font-bold">{{ formatNumber(home.applogSummary!.total) }}</div>
                <div class="mt-1 flex items-center gap-1 text-xs">
                  <span :class="home.applogSummary!.trend >= 0 ? 'text-t-green' : 'text-t-red'">
                    {{ home.applogSummary!.trend >= 0 ? '&#x25B2;' : '&#x25BC;' }}{{ Math.abs(home.applogSummary!.trend).toFixed(1) }}%
                  </span>
                  <span class="text-t-fg-dark">vs prev {{ rangeLabel }}</span>
                </div>
              </div>

              <!-- Fatal & Errors -->
              <div class="bg-t-bg-dark border-t-border rounded border p-4">
                <div class="text-t-fg-dark mb-1 text-xs uppercase tracking-wide">Fatal & Errors</div>
                <div class="text-2xl font-bold"><RouterLink :to="{ name: 'applog', query: { level_exact: 'FATAL' } }" class="text-sev-emerg hover:underline">{{ formatNumber(applogFatal) }}</RouterLink> <span class="text-t-fg-dark">/</span> <RouterLink :to="{ name: 'applog', query: { level_exact: 'ERROR' } }" class="text-sev-alert hover:underline">{{ formatNumber(applogErrors) }}</RouterLink></div>
                <div class="text-t-fg-dark mt-1 text-xs">
                  {{ home.applogSummary!.total > 0 ? ((applogFatalErrors / home.applogSummary!.total) * 100).toFixed(1) : 0 }}% of total
                </div>
              </div>

              <!-- Warnings -->
              <div class="bg-t-bg-dark border-t-border rounded border p-4">
                <div class="text-t-fg-dark mb-1 text-xs uppercase tracking-wide">Warnings</div>
                <div class="text-2xl font-bold"><RouterLink :to="{ name: 'applog', query: { level_exact: 'WARN' } }" class="text-sev-crit hover:underline">{{ formatNumber(home.applogSummary!.warnings) }}</RouterLink></div>
                <div class="text-t-fg-dark mt-1 text-xs">
                  {{ home.applogSummary!.total > 0 ? ((home.applogSummary!.warnings / home.applogSummary!.total) * 100).toFixed(1) : 0 }}% of total
                </div>
              </div>

              <!-- Info -->
              <div class="bg-t-bg-dark border-t-border rounded border p-4">
                <div class="text-t-fg-dark mb-1 text-xs uppercase tracking-wide">Info</div>
                <div class="text-2xl font-bold"><RouterLink :to="{ name: 'applog', query: { level_exact: 'INFO' } }" class="text-sev-notice hover:underline">{{ formatNumber(applogInfo) }}</RouterLink></div>
                <div class="text-t-fg-dark mt-1 text-xs">
                  {{ home.applogSummary!.total > 0 ? ((applogInfo / home.applogSummary!.total) * 100).toFixed(1) : 0 }}% of total
                </div>
              </div>
            </div>
          </div>

          <!-- Two Column Layout -->
          <div v-if="isVisible('applog-distribution')" class="relative mb-4">
            <button v-if="editing" class="absolute -right-1.5 -top-1.5 z-10 flex h-5 w-5 items-center justify-center rounded-full border border-t-border bg-t-bg-dark text-t-fg-dark transition-colors hover:border-t-red hover:text-t-red" @click="hideWidget('applog-distribution')"><svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" /></svg></button>
            <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
              <!-- Level Distribution -->
              <div class="bg-t-bg-dark border-t-border rounded border p-4">
                <h3 class="text-t-fg-dark mb-3 text-xs font-semibold uppercase tracking-wide">Level Distribution</h3>
                <div class="space-y-2">
                  <div
                    v-for="item in sortedLevelBreakdown"
                    :key="item.level"
                    class="group flex cursor-pointer items-center gap-2"
                  >
                    <span class="w-16 shrink-0 text-xs uppercase" :class="getSeverityColorClass(item.level)">{{ item.level }}</span>
                    <div class="bg-t-bg-highlight h-2 flex-1 overflow-hidden rounded">
                      <div
                        class="h-full rounded transition-all group-hover:opacity-80"
                        :class="getSeverityBgClass(item.level)"
                        :style="{ width: `${Math.min(item.pct * 1.3, 100)}%`, opacity: 0.7 }"
                      ></div>
                    </div>
                    <span class="text-t-fg-dark w-8 text-right text-xs">{{ item.pct.toFixed(0) }}%</span>
                    <span class="text-t-fg w-10 text-right text-xs">{{ formatNumber(item.count) }}</span>
                  </div>
                </div>
              </div>

              <!-- Top Services -->
              <div class="bg-t-bg-dark border-t-border flex flex-col rounded border p-4">
                <h3 class="text-t-fg-dark mb-3 text-xs font-semibold uppercase tracking-wide">Top Services</h3>
                <div ref="servicesListEl" class="min-h-[120px] flex-1 space-y-2 overflow-hidden">
                  <RouterLink
                    v-for="(service, idx) in visibleServices"
                    :key="service.name"
                    :to="{ name: 'applog', query: { service: service.name } }"
                    class="group flex cursor-pointer items-center gap-2"
                  >
                    <span class="text-t-fg-dark w-4 text-xs">{{ idx + 1 }}.</span>
                    <span class="w-28 truncate text-sm md:w-40" :style="{ color: accentColors[idx % accentColors.length] }">{{ service.name }}</span>
                    <div class="bg-t-bg-highlight h-2 flex-1 overflow-hidden rounded">
                      <div
                        class="h-full rounded transition-all group-hover:opacity-80"
                        :style="{ width: `${service.pct}%`, opacity: 0.6, backgroundColor: accentColors[idx % accentColors.length] }"
                      ></div>
                    </div>
                    <span class="text-t-fg-dark w-8 text-right text-xs">{{ service.pct.toFixed(0) }}%</span>
                    <span class="text-t-fg w-10 text-right text-xs">{{ formatNumber(service.count) }}</span>
                  </RouterLink>
                </div>
              </div>
            </div>
          </div>

          <!-- Recent Errors -->
          <div v-if="isVisible('applog-recent')" class="relative">
            <button v-if="editing" class="absolute -right-1.5 -top-1.5 z-10 flex h-5 w-5 items-center justify-center rounded-full border border-t-border bg-t-bg-dark text-t-fg-dark transition-colors hover:border-t-red hover:text-t-red" @click="hideWidget('applog-recent')"><svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" /></svg></button>
            <div class="bg-t-bg-dark border-t-border rounded border">
              <h3 class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-xs font-semibold uppercase tracking-wide">Recent Errors</h3>
              <div>
                <div v-if="home.recentApplogEvents.length === 0" class="text-t-fg-dark px-4 py-2 text-center text-xs">
                  No recent error events (error, fatal, panic)
                </div>
                <router-link
                  v-for="event in home.recentApplogEvents"
                  :key="event.id"
                  :to="`/applog/${event.id}`"
                  class="hover:bg-t-bg-hover flex cursor-pointer items-baseline gap-3 px-4 py-px leading-snug transition-colors"
                  :class="newApplogIds.has(event.id) ? 'row-flash' : ''"
                >
                  <span class="text-t-fg-dark w-[8ch] shrink-0">{{ formatTime(event.timestamp) }}</span>
                  <span class="w-[6ch] shrink-0 uppercase" :class="getSeverityColorClass(event.level)">{{ event.level }}</span>
                  <span class="text-t-teal hidden w-[22ch] shrink-0 truncate md:inline">{{ event.service }}</span>
                  <span class="text-t-purple hidden w-[18ch] shrink-0 truncate md:inline">{{ event.component }}</span>
                  <span class="text-t-fg min-w-0 flex-1 truncate">{{ event.msg }}<template v-if="event.attrs && Object.keys(event.attrs).length > 0">&nbsp;<span class="text-t-orange">-</span> <span class="text-t-fg-dark">{{ formatAttrs(event.attrs) }}</span></template></span>
                </router-link>
              </div>
            </div>
          </div>
        </template>
      </section>
      <!-- ═══════════════════════════ ACTIVITY HEATMAPS ═══════════════════════════ -->
      <section v-if="anyHeatmapVisible || editing">
        <h2 class="text-t-fg-dark mb-4 flex items-center gap-2 text-sm font-semibold uppercase tracking-wider">
          <span>Activity</span>
          <span class="bg-t-border h-px flex-1"></span>
        </h2>

        <div class="grid grid-cols-1 gap-4 xl:grid-cols-2">
          <!-- Syslog Heatmap -->
          <div v-if="isVisible('syslog-heatmap')" class="relative">
            <button v-if="editing" class="absolute -right-1.5 -top-1.5 z-10 flex h-5 w-5 items-center justify-center rounded-full border border-t-border bg-t-bg-dark text-t-fg-dark transition-colors hover:border-t-red hover:text-t-red" @click="hideWidget('syslog-heatmap')"><svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" /></svg></button>
            <div class="bg-t-bg-dark border-t-border rounded border p-4">
              <h3 class="text-t-teal mb-3 text-xs font-semibold uppercase tracking-wide">Syslog Volume</h3>
              <ActivityHeatmap :data="home.syslogHeatmap" color-var="--color-t-teal" label="syslog events" />
            </div>
          </div>

          <!-- Applog Heatmap -->
          <div v-if="isVisible('applog-heatmap')" class="relative">
            <button v-if="editing" class="absolute -right-1.5 -top-1.5 z-10 flex h-5 w-5 items-center justify-center rounded-full border border-t-border bg-t-bg-dark text-t-fg-dark transition-colors hover:border-t-red hover:text-t-red" @click="hideWidget('applog-heatmap')"><svg class="h-3 w-3" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><line x1="18" y1="6" x2="6" y2="18" /><line x1="6" y1="6" x2="18" y2="18" /></svg></button>
            <div class="bg-t-bg-dark border-t-border rounded border p-4">
              <h3 class="text-t-magenta mb-3 text-xs font-semibold uppercase tracking-wide">Applog Volume</h3>
              <ActivityHeatmap :data="home.applogHeatmap" color-var="--color-t-magenta" label="applog events" />
            </div>
          </div>
        </div>
      </section>

      <!-- Floating edit bar -->
      <Teleport to="body">
        <div v-if="editing" class="fixed bottom-4 left-1/2 z-50 flex -translate-x-1/2 items-center gap-3 rounded-lg border border-t-border bg-t-bg-dark px-4 py-2 shadow-lg">
          <span class="text-t-fg-dark text-xs">Editing dashboard</span>
          <button class="text-t-fg-dark hover:text-t-fg text-xs transition-colors" @click="resetLayout()">Reset</button>
          <button class="bg-t-teal text-t-bg rounded px-3 py-1 text-xs font-semibold transition-colors hover:brightness-110" @click="stopEditing()">Done</button>
        </div>
      </Teleport>
    </template>
  </div>
</template>

<style scoped>
.row-flash {
  animation: row-flash 1s ease-out;
}

@keyframes row-flash {
  0% { background-color: var(--color-t-bg-highlight); }
  100% { background-color: transparent; }
}
</style>
