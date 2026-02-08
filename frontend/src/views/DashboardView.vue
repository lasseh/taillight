<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { VisXYContainer, VisStackedBar, VisAxis, VisCrosshair, VisTooltip } from '@unovis/vue'
import { useDashboardStore } from '@/stores/dashboard'
import { useAppLogDashboardStore } from '@/stores/applog-dashboard'
import { useRsyslogStatsStore } from '@/stores/rsyslog-stats'
import { useTheme } from '@/composables/useTheme'
import type { VolumeDataRecord } from '@/types/stats'
import type { RsyslogStatsDataRecord } from '@/types/rsyslog-stats'

const route = useRoute()
const router = useRouter()
const dashboard = useDashboardStore()
const applogDashboard = useAppLogDashboardStore()
const rsyslogStats = useRsyslogStatsStore()
const { current: theme } = useTheme()

function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;')
}

type Tab = 'syslog' | 'applog' | 'rsyslog'
const activeTab = ref<Tab>((route.query.tab as Tab) || 'syslog')

const accentColors = computed(() => theme.value.chartColors)

const volumePresets: { label: string; range: string; interval: string }[] = [
  { label: '1h', range: '1h', interval: '1m' },
  { label: '6h', range: '6h', interval: '5m' },
  { label: '24h', range: '24h', interval: '15m' },
  { label: '7d', range: '7d', interval: '30m' },
  { label: '30d', range: '30d', interval: '6h' },
]

const rsyslogPresets: { label: string; range: string; interval: string }[] = [
  { label: '1h', range: '1h', interval: '1m' },
  { label: '6h', range: '6h', interval: '5m' },
  { label: '24h', range: '24h', interval: '15m' },
  { label: '7d', range: '7d', interval: '1h' },
]

const activePresets = computed(() => activeTab.value === 'rsyslog' ? rsyslogPresets : volumePresets)

const intervalMs: Record<string, number> = {
  '1m': 60_000,
  '5m': 300_000,
  '15m': 900_000,
  '30m': 1_800_000,
  '1h': 3_600_000,
  '6h': 21_600_000,
}

const dataStep = computed(() => {
  if (activeTab.value === 'rsyslog') return intervalMs[rsyslogStats.interval] ?? 60_000
  if (activeTab.value === 'applog') return intervalMs[applogDashboard.interval] ?? 60_000
  return intervalMs[dashboard.interval] ?? 60_000
})

const activeRange = computed(() => {
  if (activeTab.value === 'rsyslog') return rsyslogStats.range
  if (activeTab.value === 'applog') return applogDashboard.range
  return dashboard.range
})
const activeLoading = computed(() => {
  if (activeTab.value === 'rsyslog') return rsyslogStats.loading
  if (activeTab.value === 'applog') return applogDashboard.loading
  return dashboard.loading
})
const activeError = computed(() => {
  if (activeTab.value === 'rsyslog') return rsyslogStats.error
  if (activeTab.value === 'applog') return applogDashboard.error
  return dashboard.error
})

function switchTab(tab: Tab) {
  activeTab.value = tab
  router.replace({ query: { ...route.query, tab } })
  if (tab === 'syslog' && dashboard.buckets?.length === 0) {
    dashboard.fetchVolume()
  } else if (tab === 'applog' && applogDashboard.buckets?.length === 0) {
    applogDashboard.fetchVolume()
  } else if (tab === 'rsyslog' && !rsyslogStats.summary) {
    rsyslogStats.startRefresh()
  }
}

function selectPreset(range: string, interval: string) {
  if (activeTab.value === 'rsyslog') {
    rsyslogStats.setPreset(range, interval)
  } else if (activeTab.value === 'applog') {
    applogDashboard.setPreset(range, interval)
  } else {
    dashboard.setPreset(range, interval)
  }
  router.replace({ query: { ...route.query, range } })
}

// X accessor
const xTotal = (d: VolumeDataRecord) => d.x

// --- Hover state for individual charts ---
const hoveredHost = ref<Record<string, VolumeDataRecord | null>>({})
const hoveredService = ref<Record<string, VolumeDataRecord | null>>({})

function formatHoverTime(ts: number): string {
  const d = new Date(ts)
  const r = activeRange.value
  if (r === '7d' || r === '30d') {
    return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false })
  }
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false })
}

// --- Generic chart helpers ---
function makeYAccessors(keys: string[]) {
  return keys.map((k) => (d: VolumeDataRecord) => (d[k] as number) ?? 0)
}

function makeColorAccessor(_d: VolumeDataRecord, i: number) {
  return accentColors.value[i % accentColors.value.length]
}

function makeTemplate(keys: string[]) {
  return (d: VolumeDataRecord) => {
    const date = new Date(d.x)
    const lines = keys
      .map((k, i) => {
        const v = (d[k] as number) ?? 0
        if (v === 0) return ''
        const color = accentColors.value[i % accentColors.value.length]
        return `<div><span style="color:${color}">●</span> ${escapeHtml(k)}: <b>${v}</b></div>`
      })
      .filter(Boolean)
      .join('')
    return `<div style="font-family:var(--font-mono);font-size:11px;padding:4px 8px">
      <div style="color:var(--color-t-fg-dark)">${date.toLocaleString()}</div>
      ${lines}
    </div>`
  }
}

function makeSingleYAccessor(key: string) {
  return [(d: VolumeDataRecord) => (d[key] as number) ?? 0]
}

function makeSingleTracker(hovered: typeof hoveredHost, key: string) {
  return (d: VolumeDataRecord) => {
    hovered.value[key] = d
    return ''
  }
}

// Syslog-specific wrappers using generic helpers.
function hostYAccessors() { return makeYAccessors(dashboard.hosts) }
const hostColorAccessor = makeColorAccessor
function hostTemplate(d: VolumeDataRecord) { return makeTemplate(dashboard.hosts)(d) }
function singleHostYAccessor(host: string) { return makeSingleYAccessor(host) }
function singleHostTracker(host: string) { return makeSingleTracker(hoveredHost, host) }

// Applog-specific wrappers using generic helpers.
function serviceYAccessors() { return makeYAccessors(applogDashboard.services) }
const serviceColorAccessor = makeColorAccessor
function serviceTemplate(d: VolumeDataRecord) { return makeTemplate(applogDashboard.services)(d) }
function singleServiceYAccessor(service: string) { return makeSingleYAccessor(service) }
function singleServiceTracker(service: string) { return makeSingleTracker(hoveredService, service) }

// Rsyslog-specific chart helpers (uses RsyslogStatsDataRecord but same shape).
const rsyslogXAccessor = (d: RsyslogStatsDataRecord) => d.x

function rsyslogMakeYAccessors(keys: string[]) {
  return keys.map((k) => (d: RsyslogStatsDataRecord) => (d[k] as number) ?? 0)
}

function rsyslogColorAccessor(_d: RsyslogStatsDataRecord, i: number) {
  return accentColors.value[i % accentColors.value.length]
}

function rsyslogMakeTemplate(keys: string[]) {
  return (d: RsyslogStatsDataRecord) => {
    const date = new Date(d.x)
    const lines = keys
      .map((k, i) => {
        const v = (d[k] as number) ?? 0
        if (v === 0) return ''
        const color = accentColors.value[i % accentColors.value.length]
        return `<div><span style="color:${color}">●</span> ${escapeHtml(k)}: <b>${v}</b></div>`
      })
      .filter(Boolean)
      .join('')
    return `<div style="font-family:var(--font-mono);font-size:11px;padding:4px 8px">
      <div style="color:var(--color-t-fg-dark)">${date.toLocaleString()}</div>
      ${lines}
    </div>`
  }
}

// KPI formatting for rsyslog tab
function formatRate(v: number): string {
  return v.toFixed(1)
}

function formatCount(v: number): string {
  if (v >= 1_000_000) return (v / 1_000_000).toFixed(1) + 'M'
  if (v >= 1_000) return (v / 1_000).toFixed(1) + 'K'
  return String(v)
}

const xTickFormat = (v: number) => {
  const d = new Date(v)
  const r = activeRange.value
  if (r === '7d' || r === '30d') {
    return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
  }
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', hour12: false })
}

onMounted(() => {
  const tab = route.query.tab as Tab | undefined
  if (tab === 'applog') activeTab.value = 'applog'
  else if (tab === 'rsyslog') activeTab.value = 'rsyslog'

  const r = route.query.range as string | undefined

  if (activeTab.value === 'rsyslog') {
    const preset = r ? rsyslogPresets.find((p) => p.range === r) : undefined
    if (preset) {
      rsyslogStats.setPreset(preset.range, preset.interval)
    }
    rsyslogStats.startRefresh()
  } else {
    const preset = r ? volumePresets.find((p) => p.range === r) : undefined
    const store = activeTab.value === 'syslog' ? dashboard : applogDashboard
    if (preset) {
      store.setPreset(preset.range, preset.interval)
    } else {
      store.fetchVolume()
    }
  }
})

onUnmounted(() => {
  rsyslogStats.stopRefresh()
})
</script>

<template>
  <div class="flex flex-1 flex-col gap-6 overflow-y-auto p-4">
    <!-- Tab bar + Preset buttons -->
    <div class="flex items-center gap-4">
      <div class="flex gap-1">
        <button
          class="px-2 py-0.5 text-xs transition-colors"
          :class="
            activeTab === 'syslog'
              ? 'bg-t-bg-highlight text-t-teal'
              : 'text-t-fg-dark hover:text-t-fg'
          "
          @click="switchTab('syslog')"
        >
          SYSLOG
        </button>
        <button
          class="px-2 py-0.5 text-xs transition-colors"
          :class="
            activeTab === 'applog'
              ? 'bg-t-bg-highlight text-t-magenta'
              : 'text-t-fg-dark hover:text-t-fg'
          "
          @click="switchTab('applog')"
        >
          APP LOG
        </button>
        <button
          class="px-2 py-0.5 text-xs transition-colors"
          :class="
            activeTab === 'rsyslog'
              ? 'bg-t-bg-highlight text-t-orange'
              : 'text-t-fg-dark hover:text-t-fg'
          "
          @click="switchTab('rsyslog')"
        >
          RSYSLOG
        </button>
      </div>

      <span class="text-t-border">|</span>

      <span class="text-t-fg-dark text-xs">Range:</span>
      <button
        v-for="p in activePresets"
        :key="p.label"
        class="px-2 py-0.5 text-xs transition-colors"
        :class="
          activeRange === p.range
            ? 'bg-t-bg-highlight text-t-purple'
            : 'text-t-fg-dark hover:text-t-fg'
        "
        @click="selectPreset(p.range, p.interval)"
      >
        {{ p.label }}
      </button>
      <span v-if="activeLoading" class="text-t-fg-dark ml-2 text-xs">loading...</span>
      <span v-if="activeError" class="text-t-red ml-2 text-xs">{{ activeError }}</span>
      <span v-if="activeTab === 'rsyslog'" class="text-t-fg-dark ml-auto text-xs opacity-50">auto-refresh: 60s</span>
    </div>

    <!-- ═══════════════ SYSLOG TAB ═══════════════ -->
    <template v-if="activeTab === 'syslog'">
      <!-- Chart 1: Total volume (stacked bar by host) -->
      <div>
        <h3 class="text-t-fg-dark mb-2 text-xs font-semibold uppercase tracking-wide">
          Total Volume
        </h3>
        <div class="bg-t-bg-dark border-t-border rounded border p-3">
          <VisXYContainer :data="dashboard.chartData" :height="220" :padding="{ top: 8, right: 8 }">
            <VisStackedBar
              :x="xTotal"
              :y="hostYAccessors()"
              :color="hostColorAccessor"
              :barPadding="0.6"
              :roundedCorners="2"
              :dataStep="dataStep"
            />
            <VisAxis type="x" :tickFormat="xTickFormat" :gridLine="false" :tickLine="false" />
            <VisAxis type="y" :gridLine="true" :tickLine="false" />
            <VisCrosshair :template="hostTemplate" />
            <VisTooltip />
          </VisXYContainer>
        </div>

        <!-- Legend -->
        <div class="mt-2 flex flex-wrap gap-3">
          <span
            v-for="(host, i) in dashboard.hosts"
            :key="host"
            class="flex items-center gap-1 text-xs"
          >
            <span
              class="inline-block h-2.5 w-2.5 rounded-sm"
              :style="{ backgroundColor: accentColors[i % accentColors.length] }"
            />
            <span class="text-t-fg-dark">{{ host }}</span>
          </span>
        </div>
      </div>

      <!-- Chart 2: Individual bar chart per host -->
      <div class="grid grid-cols-2 gap-4 lg:grid-cols-3">
        <div v-for="(host, i) in dashboard.hosts" :key="host">
          <h3 class="text-t-fg-dark mb-1 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide">
            <span
              class="inline-block h-2 w-2 rounded-sm"
              :style="{ backgroundColor: accentColors[i % accentColors.length] }"
            />
            {{ host }}
            <template v-if="hoveredHost[host]">
              <span class="ml-auto font-normal normal-case tracking-normal opacity-60">
                {{ formatHoverTime(hoveredHost[host]!.x) }}
              </span>
              <span
                class="font-bold normal-case tracking-normal"
                :style="{ color: accentColors[i % accentColors.length] }"
              >
                {{ (hoveredHost[host]![host] as number) ?? 0 }}
              </span>
            </template>
          </h3>
          <div
            class="hide-tooltip bg-t-bg-dark border-t-border rounded border p-2"
            @mouseleave="hoveredHost[host] = null"
          >
            <VisXYContainer :data="dashboard.chartData" :height="120" :padding="{ top: 4, right: 4 }">
              <VisStackedBar
                :x="xTotal"
                :y="singleHostYAccessor(host)"
                :color="() => accentColors[i % accentColors.length]"
                :barPadding="0.6"
                :roundedCorners="2"
                :dataStep="dataStep"
              />
              <VisAxis type="x" :tickFormat="xTickFormat" :numTicks="3" :gridLine="false" :tickLine="false" />
              <VisAxis type="y" :gridLine="true" :tickLine="false" />
              <VisCrosshair :template="singleHostTracker(host)" />
              <VisTooltip />
            </VisXYContainer>
          </div>
        </div>
      </div>
    </template>

    <!-- ═══════════════ APP LOG TAB ═══════════════ -->
    <template v-if="activeTab === 'applog'">
      <!-- Chart 1: Total volume (stacked bar by service) -->
      <div>
        <h3 class="text-t-fg-dark mb-2 text-xs font-semibold uppercase tracking-wide">
          Total Volume
        </h3>
        <div class="bg-t-bg-dark border-t-border rounded border p-3">
          <VisXYContainer :data="applogDashboard.chartData" :height="220" :padding="{ top: 8, right: 8 }">
            <VisStackedBar
              :x="xTotal"
              :y="serviceYAccessors()"
              :color="serviceColorAccessor"
              :barPadding="0.6"
              :roundedCorners="2"
              :dataStep="dataStep"
            />
            <VisAxis type="x" :tickFormat="xTickFormat" :gridLine="false" :tickLine="false" />
            <VisAxis type="y" :gridLine="true" :tickLine="false" />
            <VisCrosshair :template="serviceTemplate" />
            <VisTooltip />
          </VisXYContainer>
        </div>

        <!-- Legend -->
        <div class="mt-2 flex flex-wrap gap-3">
          <span
            v-for="(service, i) in applogDashboard.services"
            :key="service"
            class="flex items-center gap-1 text-xs"
          >
            <span
              class="inline-block h-2.5 w-2.5 rounded-sm"
              :style="{ backgroundColor: accentColors[i % accentColors.length] }"
            />
            <span class="text-t-fg-dark">{{ service }}</span>
          </span>
        </div>
      </div>

      <!-- Chart 2: Individual bar chart per service -->
      <div class="grid grid-cols-2 gap-4 lg:grid-cols-3">
        <div v-for="(service, i) in applogDashboard.services" :key="service">
          <h3 class="text-t-fg-dark mb-1 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide">
            <span
              class="inline-block h-2 w-2 rounded-sm"
              :style="{ backgroundColor: accentColors[i % accentColors.length] }"
            />
            {{ service }}
            <template v-if="hoveredService[service]">
              <span class="ml-auto font-normal normal-case tracking-normal opacity-60">
                {{ formatHoverTime(hoveredService[service]!.x) }}
              </span>
              <span
                class="font-bold normal-case tracking-normal"
                :style="{ color: accentColors[i % accentColors.length] }"
              >
                {{ (hoveredService[service]![service] as number) ?? 0 }}
              </span>
            </template>
          </h3>
          <div
            class="hide-tooltip bg-t-bg-dark border-t-border rounded border p-2"
            @mouseleave="hoveredService[service] = null"
          >
            <VisXYContainer :data="applogDashboard.chartData" :height="120" :padding="{ top: 4, right: 4 }">
              <VisStackedBar
                :x="xTotal"
                :y="singleServiceYAccessor(service)"
                :color="() => accentColors[i % accentColors.length]"
                :barPadding="0.6"
                :roundedCorners="2"
                :dataStep="dataStep"
              />
              <VisAxis type="x" :tickFormat="xTickFormat" :numTicks="3" :gridLine="false" :tickLine="false" />
              <VisAxis type="y" :gridLine="true" :tickLine="false" />
              <VisCrosshair :template="singleServiceTracker(service)" />
              <VisTooltip />
            </VisXYContainer>
          </div>
        </div>
      </div>
    </template>

    <!-- ═══════════════ RSYSLOG TAB ═══════════════ -->
    <template v-if="activeTab === 'rsyslog'">
      <!-- KPI Cards -->
      <div v-if="rsyslogStats.summary" class="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <div class="bg-t-bg-dark border-t-border rounded border p-3">
          <div class="text-t-fg-dark text-[10px] font-semibold uppercase tracking-wider">Ingest Rate</div>
          <div class="text-t-fg mt-1 text-xl font-bold">{{ formatRate(rsyslogStats.summary.ingest_rate) }}</div>
          <div class="text-t-fg-dark text-xs">msgs/min</div>
        </div>
        <div class="bg-t-bg-dark border-t-border rounded border p-3">
          <div class="text-t-fg-dark text-[10px] font-semibold uppercase tracking-wider">Filter Rate</div>
          <div class="text-t-fg mt-1 text-xl font-bold">{{ formatRate(rsyslogStats.summary.filter_rate) }}%</div>
          <div class="text-t-fg-dark text-xs">submitted not processed</div>
        </div>
        <div class="bg-t-bg-dark border-t-border rounded border p-3">
          <div class="text-t-fg-dark text-[10px] font-semibold uppercase tracking-wider">Failures</div>
          <div class="mt-1 text-xl font-bold" :class="rsyslogStats.summary.total_failed > 0 ? 'text-t-red' : 'text-t-fg'">
            {{ formatCount(rsyslogStats.summary.total_failed) }}
          </div>
          <div class="text-t-fg-dark text-xs">
            {{ formatCount(rsyslogStats.summary.total_suspended) }} suspended
          </div>
        </div>
        <div class="bg-t-bg-dark border-t-border rounded border p-3">
          <div class="text-t-fg-dark text-[10px] font-semibold uppercase tracking-wider">Queue Health</div>
          <div class="text-t-fg mt-1 text-xl font-bold">{{ formatCount(rsyslogStats.summary.main_queue_size) }}</div>
          <div class="text-t-fg-dark text-xs">
            max {{ formatCount(rsyslogStats.summary.main_queue_max_size) }}
            <template v-if="rsyslogStats.summary.total_discarded > 0">
              &middot; <span class="text-t-red">{{ formatCount(rsyslogStats.summary.total_discarded) }} discarded</span>
            </template>
          </div>
        </div>
      </div>

      <!-- Charts: 2x2 grid -->
      <div class="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <!-- Ingest Volume -->
        <div>
          <h3 class="text-t-fg-dark mb-2 text-xs font-semibold uppercase tracking-wide">Ingest Volume</h3>
          <div class="bg-t-bg-dark border-t-border rounded border p-3">
            <VisXYContainer :data="rsyslogStats.ingestChartData" :height="200" :padding="{ top: 8, right: 8 }">
              <VisStackedBar
                :x="rsyslogXAccessor"
                :y="rsyslogMakeYAccessors(rsyslogStats.ingestNames)"
                :color="rsyslogColorAccessor"
                :barPadding="0.6"
                :roundedCorners="2"
                :dataStep="dataStep"
              />
              <VisAxis type="x" :tickFormat="xTickFormat" :gridLine="false" :tickLine="false" />
              <VisAxis type="y" :gridLine="true" :tickLine="false" />
              <VisCrosshair :template="rsyslogMakeTemplate(rsyslogStats.ingestNames)" />
              <VisTooltip />
            </VisXYContainer>
          </div>
          <div class="mt-2 flex flex-wrap gap-3">
            <span v-for="(name, i) in rsyslogStats.ingestNames" :key="name" class="flex items-center gap-1 text-xs">
              <span class="inline-block h-2.5 w-2.5 rounded-sm" :style="{ backgroundColor: accentColors[i % accentColors.length] }" />
              <span class="text-t-fg-dark">{{ name }}</span>
            </span>
          </div>
        </div>

        <!-- Queue Depth -->
        <div>
          <h3 class="text-t-fg-dark mb-2 text-xs font-semibold uppercase tracking-wide">Queue Depth</h3>
          <div class="bg-t-bg-dark border-t-border rounded border p-3">
            <VisXYContainer :data="rsyslogStats.queueChartData" :height="200" :padding="{ top: 8, right: 8 }">
              <VisStackedBar
                :x="rsyslogXAccessor"
                :y="rsyslogMakeYAccessors(rsyslogStats.queueNames)"
                :color="rsyslogColorAccessor"
                :barPadding="0.6"
                :roundedCorners="2"
                :dataStep="dataStep"
              />
              <VisAxis type="x" :tickFormat="xTickFormat" :gridLine="false" :tickLine="false" />
              <VisAxis type="y" :gridLine="true" :tickLine="false" />
              <VisCrosshair :template="rsyslogMakeTemplate(rsyslogStats.queueNames)" />
              <VisTooltip />
            </VisXYContainer>
          </div>
          <div class="mt-2 flex flex-wrap gap-3">
            <span v-for="(name, i) in rsyslogStats.queueNames" :key="name" class="flex items-center gap-1 text-xs">
              <span class="inline-block h-2.5 w-2.5 rounded-sm" :style="{ backgroundColor: accentColors[i % accentColors.length] }" />
              <span class="text-t-fg-dark">{{ name }}</span>
            </span>
          </div>
        </div>

        <!-- Action Throughput -->
        <div>
          <h3 class="text-t-fg-dark mb-2 text-xs font-semibold uppercase tracking-wide">Action Throughput</h3>
          <div class="bg-t-bg-dark border-t-border rounded border p-3">
            <VisXYContainer :data="rsyslogStats.processedChartData" :height="200" :padding="{ top: 8, right: 8 }">
              <VisStackedBar
                :x="rsyslogXAccessor"
                :y="rsyslogMakeYAccessors(rsyslogStats.processedNames)"
                :color="rsyslogColorAccessor"
                :barPadding="0.6"
                :roundedCorners="2"
                :dataStep="dataStep"
              />
              <VisAxis type="x" :tickFormat="xTickFormat" :gridLine="false" :tickLine="false" />
              <VisAxis type="y" :gridLine="true" :tickLine="false" />
              <VisCrosshair :template="rsyslogMakeTemplate(rsyslogStats.processedNames)" />
              <VisTooltip />
            </VisXYContainer>
          </div>
          <div class="mt-2 flex flex-wrap gap-3">
            <span v-for="(name, i) in rsyslogStats.processedNames" :key="name" class="flex items-center gap-1 text-xs">
              <span class="inline-block h-2.5 w-2.5 rounded-sm" :style="{ backgroundColor: accentColors[i % accentColors.length] }" />
              <span class="text-t-fg-dark">{{ name }}</span>
            </span>
          </div>
        </div>

        <!-- Failures & Suspensions -->
        <div>
          <h3 class="text-t-fg-dark mb-2 text-xs font-semibold uppercase tracking-wide">Failures &amp; Suspensions</h3>
          <div class="bg-t-bg-dark border-t-border rounded border p-3">
            <VisXYContainer :data="rsyslogStats.failedChartData" :height="200" :padding="{ top: 8, right: 8 }">
              <VisStackedBar
                :x="rsyslogXAccessor"
                :y="rsyslogMakeYAccessors(rsyslogStats.failedNames)"
                :color="rsyslogColorAccessor"
                :barPadding="0.6"
                :roundedCorners="2"
                :dataStep="dataStep"
              />
              <VisAxis type="x" :tickFormat="xTickFormat" :gridLine="false" :tickLine="false" />
              <VisAxis type="y" :gridLine="true" :tickLine="false" />
              <VisCrosshair :template="rsyslogMakeTemplate(rsyslogStats.failedNames)" />
              <VisTooltip />
            </VisXYContainer>
          </div>
          <div class="mt-2 flex flex-wrap gap-3">
            <span v-for="(name, i) in rsyslogStats.failedNames" :key="name" class="flex items-center gap-1 text-xs">
              <span class="inline-block h-2.5 w-2.5 rounded-sm" :style="{ backgroundColor: accentColors[i % accentColors.length] }" />
              <span class="text-t-fg-dark">{{ name }}</span>
            </span>
          </div>
        </div>
      </div>

      <!-- Component Table -->
      <div v-if="rsyslogStats.summary && rsyslogStats.summary.components.length > 0">
        <h3 class="text-t-fg-dark mb-2 text-xs font-semibold uppercase tracking-wide">Components</h3>
        <div class="bg-t-bg-dark border-t-border overflow-x-auto rounded border">
          <table class="w-full text-left text-xs">
            <thead>
              <tr class="border-t-border border-b">
                <th class="text-t-fg-dark px-3 py-2 font-semibold">Origin</th>
                <th class="text-t-fg-dark px-3 py-2 font-semibold">Name</th>
                <th class="text-t-fg-dark px-3 py-2 font-semibold text-right">Submitted</th>
                <th class="text-t-fg-dark px-3 py-2 font-semibold text-right">Processed</th>
                <th class="text-t-fg-dark px-3 py-2 font-semibold text-right">Failed</th>
                <th class="text-t-fg-dark px-3 py-2 font-semibold text-right">Size</th>
                <th class="text-t-fg-dark px-3 py-2 font-semibold text-right">Max Q</th>
                <th class="text-t-fg-dark px-3 py-2 font-semibold">Last Collected</th>
              </tr>
            </thead>
            <tbody>
              <tr
                v-for="(comp, idx) in rsyslogStats.summary.components"
                :key="`${comp.origin}-${comp.name}`"
                class="border-t-border transition-colors hover:bg-white/[0.02]"
                :class="idx < rsyslogStats.summary.components.length - 1 ? 'border-b' : ''"
              >
                <td class="text-t-orange px-3 py-1.5">{{ comp.origin }}</td>
                <td class="text-t-fg px-3 py-1.5">{{ comp.name }}</td>
                <td class="text-t-fg-dark px-3 py-1.5 text-right font-mono">{{ comp.stats.submitted ?? '-' }}</td>
                <td class="text-t-fg-dark px-3 py-1.5 text-right font-mono">{{ comp.stats.processed ?? '-' }}</td>
                <td class="px-3 py-1.5 text-right font-mono" :class="(comp.stats.failed ?? 0) > 0 ? 'text-t-red' : 'text-t-fg-dark'">{{ comp.stats.failed ?? '-' }}</td>
                <td class="text-t-fg-dark px-3 py-1.5 text-right font-mono">{{ comp.stats.size ?? '-' }}</td>
                <td class="text-t-fg-dark px-3 py-1.5 text-right font-mono">{{ comp.stats.maxqsize ?? '-' }}</td>
                <td class="text-t-fg-dark px-3 py-1.5">{{ new Date(comp.collected_at).toLocaleTimeString() }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
:deep(.unovis-xy-container) svg text {
  fill: var(--color-t-fg-dark);
}

:deep(.unovis-xy-container) path[class*="-bar"] {
  opacity: 0.55;
}

:deep(.unovis-xy-container) .tick line {
  stroke: var(--color-t-border);
  opacity: 0.4;
}

.hide-tooltip :deep(.unovis-tooltip) {
  display: none !important;
}
</style>
