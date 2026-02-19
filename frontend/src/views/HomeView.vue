<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { useElementSize } from '@vueuse/core'
import { useHomeStore } from '@/stores/home'
import { useTheme } from '@/composables/useTheme'
import { useNewFlash } from '@/composables/useNewFlash'
import { formatTime, formatAttrs, formatNumber } from '@/lib/format'
import { severityColorClassByLabel } from '@/lib/constants'
import { levelColorClass } from '@/lib/applog-constants'
import RecentCriticalLogs from '@/components/RecentCriticalLogs.vue'
import ActivityHeatmap from '@/components/ActivityHeatmap.vue'

defineOptions({ name: 'HomeView' })

const home = useHomeStore()
const { current: theme } = useTheme()
const accentColors = computed(() => theme.value.chartColors)

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
const newSyslogIds = useNewFlash(() => home.recentSyslogEvents)
const newApplogIds = useNewFlash(() => home.recentApplogEvents)

onMounted(() => {
  home.startRefresh()
})

onUnmounted(() => {
  home.stopRefresh()
})

// ═══════════════════════════════════════════════════════════════════════════
// Helpers
// ═══════════════════════════════════════════════════════════════════════════

function getSeverityColorClass(level: string): string {
  return levelColorClass[level] ?? severityColorClassByLabel[level.toLowerCase()] ?? 'text-t-fg'
}

// ═══════════════════════════════════════════════════════════════════════════
// Fake heatmap data (placeholder until real API)
// ═══════════════════════════════════════════════════════════════════════════

function generateFakeHeatmap(seed: number): Record<string, number> {
  const data: Record<string, number> = {}
  const today = new Date()
  today.setHours(0, 0, 0, 0)

  // Simple seeded pseudo-random
  let s = seed
  function rand() {
    s = (s * 1664525 + 1013904223) & 0x7fffffff
    return s / 0x7fffffff
  }

  for (let i = 365; i >= 0; i--) {
    const d = new Date(today.getTime() - i * 86_400_000)
    const dow = d.getDay()
    const iso = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`

    // Base rate: higher on weekdays
    let base = dow === 0 || dow === 6 ? 20 : 80
    // Monthly variation (simulate busier months)
    const month = d.getMonth()
    if (month >= 9 || month <= 1) base *= 1.4 // busier in Oct-Feb
    // Random variation
    const count = Math.floor(base * (0.2 + rand() * 1.8))
    // ~10% chance of zero days
    if (rand() < 0.1) {
      data[iso] = 0
    } else {
      data[iso] = count
    }
  }

  return data
}

const fakeSyslogHeatmap = generateFakeHeatmap(42)
const fakeApplogHeatmap = generateFakeHeatmap(137)
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

      <!-- ═══════════════════════════ SYSLOG SECTION ═══════════════════════════ -->
      <section>
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
          <!-- Stats Bar -->
          <div class="bg-t-bg-dark border-t-border mb-4 flex divide-x divide-t-border overflow-hidden rounded border tabular-nums">
            <!-- Total (hero) -->
            <div class="flex w-36 shrink-0 flex-col justify-center px-4 py-3">
              <div class="text-t-fg-dark text-[10px] uppercase tracking-wide">Total {{ rangeLabel }}</div>
              <div class="text-t-teal text-2xl font-bold">{{ formatNumber(home.syslogSummary!.total) }}</div>
              <div class="flex items-center gap-1 text-[10px]">
                <span :class="home.syslogSummary!.trend >= 0 ? 'text-t-green' : 'text-t-red'">
                  {{ home.syslogSummary!.trend >= 0 ? '&#x25B2;' : '&#x25BC;' }}{{ Math.abs(home.syslogSummary!.trend).toFixed(1) }}%
                </span>
                <span class="text-t-fg-dark">vs prev</span>
              </div>
            </div>
            <!-- Emerg -->
            <div class="flex flex-1 flex-col items-center justify-center py-3">
              <div class="text-t-fg-dark text-[10px] uppercase tracking-wide">Emerg</div>
              <div class="text-sev-emerg text-xl font-bold leading-snug">{{ formatNumber(syslogEmerg) }}</div>
            </div>
            <!-- Alert -->
            <div class="flex flex-1 flex-col items-center justify-center py-3">
              <div class="text-t-fg-dark text-[10px] uppercase tracking-wide">Alert</div>
              <div class="text-sev-alert text-xl font-bold leading-snug">{{ formatNumber(syslogAlert) }}</div>
            </div>
            <!-- Crit -->
            <div class="flex flex-1 flex-col items-center justify-center py-3">
              <div class="text-t-fg-dark text-[10px] uppercase tracking-wide">Crit</div>
              <div class="text-sev-crit text-xl font-bold leading-snug">{{ formatNumber(syslogCriticals) }}</div>
            </div>
            <!-- Error -->
            <div class="flex flex-1 flex-col items-center justify-center py-3">
              <div class="text-t-fg-dark text-[10px] uppercase tracking-wide">Error</div>
              <div class="text-sev-err text-xl font-bold leading-snug">{{ formatNumber(syslogErrors) }}</div>
            </div>
          </div>

          <!-- Two Column Layout -->
          <div class="mb-4 grid grid-cols-1 gap-4 md:grid-cols-2">
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

            <!-- Heatmap -->
            <div class="bg-t-bg-dark border-t-border rounded border p-4">
              <ActivityHeatmap :data="fakeSyslogHeatmap" color-var="--color-t-teal" label="syslog events" />
            </div>
          </div>

          <!-- Recent High-Severity Events -->
          <RecentCriticalLogs :events="home.recentSyslogEvents" show-hostname :flash-ids="newSyslogIds" />
        </template>
      </section>

      <!-- ═══════════════════════════ APPLOG SECTION ═══════════════════════════ -->
      <section>
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
          <!-- Stats Bar -->
          <div class="bg-t-bg-dark border-t-border mb-4 flex divide-x divide-t-border overflow-hidden rounded border tabular-nums">
            <!-- Total (hero) -->
            <div class="flex w-36 shrink-0 flex-col justify-center px-4 py-3">
              <div class="text-t-fg-dark text-[10px] uppercase tracking-wide">Total {{ rangeLabel }}</div>
              <div class="text-t-teal text-2xl font-bold">{{ formatNumber(home.applogSummary!.total) }}</div>
              <div class="flex items-center gap-1 text-[10px]">
                <span :class="home.applogSummary!.trend >= 0 ? 'text-t-green' : 'text-t-red'">
                  {{ home.applogSummary!.trend >= 0 ? '&#x25B2;' : '&#x25BC;' }}{{ Math.abs(home.applogSummary!.trend).toFixed(1) }}%
                </span>
                <span class="text-t-fg-dark">vs prev</span>
              </div>
            </div>
            <!-- Fatal -->
            <div class="flex flex-1 flex-col items-center justify-center py-3">
              <div class="text-t-fg-dark text-[10px] uppercase tracking-wide">Fatal</div>
              <div class="text-sev-emerg text-xl font-bold leading-snug">{{ formatNumber(applogFatal) }}</div>
            </div>
            <!-- Error -->
            <div class="flex flex-1 flex-col items-center justify-center py-3">
              <div class="text-t-fg-dark text-[10px] uppercase tracking-wide">Error</div>
              <div class="text-sev-err text-xl font-bold leading-snug">{{ formatNumber(applogErrors) }}</div>
            </div>
            <!-- Warn -->
            <div class="flex flex-1 flex-col items-center justify-center py-3">
              <div class="text-t-fg-dark text-[10px] uppercase tracking-wide">Warn</div>
              <div class="text-sev-warning text-xl font-bold leading-snug">{{ formatNumber(home.applogSummary!.warnings) }}</div>
            </div>
            <!-- Info -->
            <div class="flex flex-1 flex-col items-center justify-center py-3">
              <div class="text-t-fg-dark text-[10px] uppercase tracking-wide">Info</div>
              <div class="text-sev-info text-xl font-bold leading-snug">{{ formatNumber(applogInfo) }}</div>
            </div>
          </div>

          <!-- Two Column Layout -->
          <div class="mb-4 grid grid-cols-1 gap-4 md:grid-cols-2">
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

            <!-- Heatmap -->
            <div class="bg-t-bg-dark border-t-border rounded border p-4">
              <ActivityHeatmap :data="fakeApplogHeatmap" color-var="--color-t-magenta" label="applog events" />
            </div>
          </div>

          <!-- Recent Errors -->
          <div class="bg-t-bg-dark border-t-border rounded border">
            <h3 class="text-t-fg-dark border-t-border border-b px-4 py-1.5 text-xs font-semibold uppercase tracking-wide">Recent Errors</h3>
            <div>
              <div v-if="home.recentApplogEvents.length === 0" class="text-t-fg-dark px-4 py-2 text-xs">
                No recent error events
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
        </template>
      </section>
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
