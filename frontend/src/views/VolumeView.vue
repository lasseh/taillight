<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, onBeforeUnmount, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { VisXYContainer, VisStackedBar, VisLine, VisAxis, VisCrosshair, VisTooltip } from '@unovis/vue'
import LoadingIndicator from '@/components/LoadingIndicator.vue'
import { useSrvlogVolumeStore } from '@/stores/srvlog-volume'
import { useNetlogVolumeStore } from '@/stores/netlog-volume'
import { useAppLogVolumeStore } from '@/stores/applog-volume'
import { useRsyslogStatsStore } from '@/stores/rsyslog-stats'
import { useTaillightMetricsStore } from '@/stores/taillight-metrics'
import { useTheme } from '@/composables/useTheme'
import { features as getFeatures } from '@/lib/features'
import type { VolumeDataRecord } from '@/types/stats'
import type { SimplePoint } from '@/types/chart'

const route = useRoute()
const router = useRouter()
const srvlogVolume = useSrvlogVolumeStore()
const netlogVolume = useNetlogVolumeStore()
const applogVolume = useAppLogVolumeStore()
const rsyslogStats = useRsyslogStatsStore()
const taillightMetrics = useTaillightMetricsStore()
const { current: theme } = useTheme()
const features = getFeatures()

function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;')
}

type Tab = 'netlog' | 'srvlog' | 'applog' | 'rsyslog' | 'taillight'
const defaultTab = features.netlog ? 'netlog' : features.srvlog ? 'srvlog' : 'applog'
const activeTab = ref<Tab>((route.query.tab as Tab) || defaultTab)

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

const taillightPresets: { label: string; range: string; interval: string }[] = [
  { label: '1h', range: '1h', interval: '1m' },
  { label: '6h', range: '6h', interval: '5m' },
  { label: '24h', range: '24h', interval: '15m' },
  { label: '7d', range: '7d', interval: '1h' },
]

/** Resolve the best interval for a given range, respecting tab-specific presets. */
function rangeToInterval(range: string): string {
  for (const p of volumePresets) {
    if (p.range === range) return p.interval
  }
  for (const p of rsyslogPresets) {
    if (p.range === range) return p.interval
  }
  return '15m'
}

// Shared range/interval across all tabs
const selectedRange = ref((route.query.range as string) || '24h')
const selectedInterval = ref(rangeToInterval(selectedRange.value))

const activePresets = computed(() => {
  if (activeTab.value === 'rsyslog') return rsyslogPresets
  if (activeTab.value === 'taillight') return taillightPresets
  return volumePresets
})

const intervalMs: Record<string, number> = {
  '1m': 60_000,
  '5m': 300_000,
  '15m': 900_000,
  '30m': 1_800_000,
  '1h': 3_600_000,
  '6h': 21_600_000,
}

const dataStep = computed(() => intervalMs[selectedInterval.value] ?? 60_000)

const activeRange = computed(() => selectedRange.value)
const activeLoading = computed(() => {
  if (activeTab.value === 'taillight') return taillightMetrics.loading
  if (activeTab.value === 'rsyslog') return rsyslogStats.loading
  if (activeTab.value === 'applog') return applogVolume.loading
  if (activeTab.value === 'netlog') return netlogVolume.loading
  return srvlogVolume.loading
})
const activeError = computed(() => {
  if (activeTab.value === 'taillight') return taillightMetrics.error
  if (activeTab.value === 'rsyslog') return rsyslogStats.error
  if (activeTab.value === 'applog') return applogVolume.error
  if (activeTab.value === 'netlog') return netlogVolume.error
  return srvlogVolume.error
})

/** Sync a store to the shared range (using the appropriate interval for its preset set). */
function syncStore(tab: Tab) {
  const r = selectedRange.value
  const i = selectedInterval.value
  // rsyslog/taillight cap at 7d; clamp to their max if needed
  const rsI = rsyslogPresets.find(p => p.range === r)?.interval
  const tlI = taillightPresets.find(p => p.range === r)?.interval

  if (tab === 'netlog') netlogVolume.setPreset(r, i)
  else if (tab === 'srvlog') srvlogVolume.setPreset(r, i)
  else if (tab === 'applog') applogVolume.setPreset(r, i)
  else if (tab === 'rsyslog' && rsI) rsyslogStats.setPreset(r, rsI)
  else if (tab === 'taillight' && tlI) taillightMetrics.setPreset(r, tlI)
}

function switchTab(tab: Tab) {
  activeTab.value = tab
  router.replace({ query: { ...route.query, tab, range: selectedRange.value } })
  // Ensure the tab's store has data at the current range
  if (tab === 'netlog' && (netlogVolume.buckets?.length === 0 || netlogVolume.range !== selectedRange.value)) {
    syncStore('netlog')
  } else if (tab === 'srvlog' && (srvlogVolume.buckets?.length === 0 || srvlogVolume.range !== selectedRange.value)) {
    syncStore('srvlog')
  } else if (tab === 'applog' && (applogVolume.buckets?.length === 0 || applogVolume.range !== selectedRange.value)) {
    syncStore('applog')
  } else if (tab === 'rsyslog' && (!rsyslogStats.summary || rsyslogStats.range !== selectedRange.value)) {
    if (rsyslogPresets.some(p => p.range === selectedRange.value)) syncStore('rsyslog')
    else rsyslogStats.startRefresh()
  } else if (tab === 'taillight' && (!taillightMetrics.summary || taillightMetrics.range !== selectedRange.value)) {
    if (taillightPresets.some(p => p.range === selectedRange.value)) syncStore('taillight')
    else taillightMetrics.startRefresh()
  }
}

function selectPreset(range: string, interval: string) {
  selectedRange.value = range
  selectedInterval.value = interval
  syncStore(activeTab.value)
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

// Srvlog-specific wrappers using generic helpers.
function hostYAccessors() { return makeYAccessors(srvlogVolume.hosts) }
const hostColorAccessor = makeColorAccessor
function hostTemplate(d: VolumeDataRecord) { return makeTemplate(srvlogVolume.hosts)(d) }
function singleHostYAccessor(host: string) { return makeSingleYAccessor(host) }
function singleHostTracker(host: string) { return makeSingleTracker(hoveredHost, host) }

// Netlog-specific wrappers using generic helpers.
function netlogHostYAccessors() { return makeYAccessors(netlogVolume.hosts) }
const netlogHostColorAccessor = makeColorAccessor
function netlogHostTemplate(d: VolumeDataRecord) { return makeTemplate(netlogVolume.hosts)(d) }
function singleNetlogHostYAccessor(host: string) { return makeSingleYAccessor(host) }
function singleNetlogHostTracker(host: string) { return makeSingleTracker(hoveredHost, host) }

// Applog-specific wrappers using generic helpers.
function serviceYAccessors() { return makeYAccessors(applogVolume.services) }
const serviceColorAccessor = makeColorAccessor
function serviceTemplate(d: VolumeDataRecord) { return makeTemplate(applogVolume.services)(d) }
function singleServiceYAccessor(service: string) { return makeSingleYAccessor(service) }
function singleServiceTracker(service: string) { return makeSingleTracker(hoveredService, service) }

// --- Rsyslog merged line chart data ---
// Unovis scales the y-axis from the container :data only. With separate :data
// on child VisLine components, the second series can fall outside the visible
// range. Merging into one record array ensures the y-axis covers all values.

type RsMsgRecord = { x: number; received: number; written: number }

function mergeTwoLines(a: SimplePoint[], b: SimplePoint[]): Map<number, [number, number]> {
  const m = new Map<number, [number, number]>()
  for (const p of a) m.set(p.x, [p.y, 0])
  for (const p of b) {
    const e = m.get(p.x)
    if (e) e[1] = p.y
    else m.set(p.x, [0, p.y])
  }
  return m
}

const rsMessagesData = computed<RsMsgRecord[]>(() => {
  const m = mergeTwoLines(rsyslogStats.submittedLine, rsyslogStats.processedLine)
  return [...m.entries()]
    .map(([x, [received, written]]) => ({ x, received, written }))
    .sort((a, b) => a.x - b.x)
})

const rsMessagesX = (d: RsMsgRecord) => d.x
const rsReceivedY = (d: RsMsgRecord) => d.received
const rsWrittenY = (d: RsMsgRecord) => d.written

function rsMessagesTooltip(d: RsMsgRecord) {
  return `<div style="font-family:var(--font-mono);font-size:11px;padding:4px 8px">
    <div style="color:var(--color-t-fg-dark)">${formatHoverTime(d.x)}</div>
    <div><span style="color:${accentColors.value[0]}">●</span> Received: <b>${d.received.toFixed(1)}</b></div>
    <div><span style="color:${accentColors.value[1]}">●</span> Written: <b>${d.written.toFixed(1)}</b></div>
  </div>`
}

// Queue depth (single line, keep SimplePoint)
const lineX = (d: SimplePoint) => d.x
const lineY = (d: SimplePoint) => d.y

function rsQueueTooltip(d: SimplePoint) {
  return `<div style="font-family:var(--font-mono);font-size:11px;padding:4px 8px">
    <div style="color:var(--color-t-fg-dark)">${formatHoverTime(d.x)}</div>
    <div><span style="color:${accentColors.value[2]}">●</span> Queue: <b>${d.y.toFixed(1)}</b></div>
  </div>`
}

// --- Taillight merged line chart data ---

type TlDualRecord = { x: number; srvlog: number; applog: number }
type TlPoolRecord = { x: number; active: number; idle: number; total: number }

const tlEventsData = computed<TlDualRecord[]>(() => {
  const m = mergeTwoLines(taillightMetrics.eventsBroadcastLine, taillightMetrics.applogBroadcastLine)
  return [...m.entries()]
    .map(([x, [srvlog, applog]]) => ({ x, srvlog, applog }))
    .sort((a, b) => a.x - b.x)
})

const tlSseData = computed<TlDualRecord[]>(() => {
  const m = mergeTwoLines(taillightMetrics.sseClientsSrvlogLine, taillightMetrics.sseClientsApplogLine)
  return [...m.entries()]
    .map(([x, [srvlog, applog]]) => ({ x, srvlog, applog }))
    .sort((a, b) => a.x - b.x)
})

const tlPoolData = computed<TlPoolRecord[]>(() => {
  const active = taillightMetrics.dbPoolActiveLine
  const idle = taillightMetrics.dbPoolIdleLine
  const total = taillightMetrics.dbPoolTotalLine
  const m = new Map<number, TlPoolRecord>()
  for (const p of active) m.set(p.x, { x: p.x, active: p.y, idle: 0, total: 0 })
  for (const p of idle) {
    const e = m.get(p.x)
    if (e) e.idle = p.y
    else m.set(p.x, { x: p.x, active: 0, idle: p.y, total: 0 })
  }
  for (const p of total) {
    const e = m.get(p.x)
    if (e) e.total = p.y
    else m.set(p.x, { x: p.x, active: 0, idle: 0, total: p.y })
  }
  return [...m.values()].sort((a, b) => a.x - b.x)
})

const tlDualX = (d: TlDualRecord) => d.x
const tlSrvlogY = (d: TlDualRecord) => d.srvlog
const tlApplogY = (d: TlDualRecord) => d.applog
const tlPoolX = (d: TlPoolRecord) => d.x
const tlActiveY = (d: TlPoolRecord) => d.active
const tlIdleY = (d: TlPoolRecord) => d.idle
const tlTotalY = (d: TlPoolRecord) => d.total

function tlEventsTooltip(d: TlDualRecord) {
  return `<div style="font-family:var(--font-mono);font-size:11px;padding:4px 8px">
    <div style="color:var(--color-t-fg-dark)">${formatHoverTime(d.x)}</div>
    <div><span style="color:${accentColors.value[0]}">●</span> Srvlog: <b>${d.srvlog.toFixed(1)}</b></div>
    <div><span style="color:${accentColors.value[1]}">●</span> Applog: <b>${d.applog.toFixed(1)}</b></div>
  </div>`
}

function tlSseTooltip(d: TlDualRecord) {
  return `<div style="font-family:var(--font-mono);font-size:11px;padding:4px 8px">
    <div style="color:var(--color-t-fg-dark)">${formatHoverTime(d.x)}</div>
    <div><span style="color:${accentColors.value[0]}">●</span> Srvlog: <b>${d.srvlog}</b></div>
    <div><span style="color:${accentColors.value[1]}">●</span> Applog: <b>${d.applog}</b></div>
  </div>`
}

function tlPoolTooltip(d: TlPoolRecord) {
  return `<div style="font-family:var(--font-mono);font-size:11px;padding:4px 8px">
    <div style="color:var(--color-t-fg-dark)">${formatHoverTime(d.x)}</div>
    <div><span style="color:${accentColors.value[0]}">●</span> Active: <b>${d.active}</b></div>
    <div><span style="color:${accentColors.value[1]}">●</span> Idle: <b>${d.idle}</b></div>
    <div><span style="color:${accentColors.value[2]}">●</span> Total: <b>${d.total}</b></div>
  </div>`
}

// Rsyslog component grouping
const rsyslogInputs = computed(() =>
  (rsyslogStats.summary?.components ?? [])
    .filter((c) => ['imudp', 'imtcp', 'imptcp'].includes(c.origin))
    .filter((c) => !/\(w\d+\)/.test(c.name) && !/^w\d+\//.test(c.name))
    .map((c) => ({ name: c.name, received: c.stats['submitted'] ?? c.stats['msgs.received'] ?? 0 })),
)
const rsyslogOutputs = computed(() =>
  (rsyslogStats.summary?.components ?? [])
    .filter((c) => (c.stats.processed ?? 0) > 0 || (c.stats.failed ?? 0) > 0)
    .filter((c) => !['imudp', 'imtcp', 'imptcp'].includes(c.origin))
    .map((c) => ({
      name: c.name,
      processed: c.stats.processed ?? 0,
      failed: c.stats.failed ?? 0,
      suspended: c.stats.suspended ?? 0,
    })),
)
const rsyslogFiltered = computed(() => {
  const s = rsyslogStats.summary
  if (!s) return 0
  return Math.max(0, s.total_submitted - s.total_processed)
})

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

// Shown as an overlay on rsyslog tiles while a refetch is in flight.
const rsyslogTileLoading = computed(
  () => activeTab.value === 'rsyslog' && rsyslogStats.loading,
)

// Flip the taillight poll rate based on tab visibility so SSE client counts
// refresh quickly when the tab is active and relax back to 60s otherwise.
watch(
  activeTab,
  (tab) => {
    if (tab === 'taillight') taillightMetrics.activate()
    else taillightMetrics.deactivate()
  },
  { immediate: true },
)

onMounted(() => {
  // Sync the active tab's store to the shared range and fetch
  if (activeTab.value === 'rsyslog') {
    if (rsyslogPresets.some(p => p.range === selectedRange.value)) syncStore('rsyslog')
    rsyslogStats.startRefresh()
  } else if (activeTab.value === 'taillight') {
    if (taillightPresets.some(p => p.range === selectedRange.value)) syncStore('taillight')
    taillightMetrics.startRefresh()
  } else {
    syncStore(activeTab.value)
  }
})

onBeforeUnmount(() => {
  // Always drop back to the idle interval so leaving the dashboard doesn't
  // leave a 5s poll running for the taillight store.
  taillightMetrics.deactivate()
})

onUnmounted(() => {
  rsyslogStats.stopRefresh()
  taillightMetrics.stopRefresh()
})
</script>

<template>
  <div class="flex flex-1 flex-col gap-6 overflow-y-auto p-4">
    <!-- Tab bar + Preset buttons -->
    <div class="flex items-center gap-4">
      <div class="flex gap-1">
        <button
          v-if="features.netlog"
          class="px-2 py-0.5 text-xs transition-colors"
          :class="
            activeTab === 'netlog'
              ? 'bg-t-bg-highlight text-t-purple'
              : 'text-t-fg-dark hover:text-t-fg'
          "
          @click="switchTab('netlog')"
        >
          NETLOG
        </button>
        <button
          v-if="features.srvlog"
          class="px-2 py-0.5 text-xs transition-colors"
          :class="
            activeTab === 'srvlog'
              ? 'bg-t-bg-highlight text-t-teal'
              : 'text-t-fg-dark hover:text-t-fg'
          "
          @click="switchTab('srvlog')"
        >
          SRVLOG
        </button>
        <button
          v-if="features.applog"
          class="px-2 py-0.5 text-xs transition-colors"
          :class="
            activeTab === 'applog'
              ? 'bg-t-bg-highlight text-t-magenta'
              : 'text-t-fg-dark hover:text-t-fg'
          "
          @click="switchTab('applog')"
        >
          APPLOG
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
        <button
          class="px-2 py-0.5 text-xs transition-colors"
          :class="
            activeTab === 'taillight'
              ? 'bg-t-bg-highlight text-t-purple'
              : 'text-t-fg-dark hover:text-t-fg'
          "
          @click="switchTab('taillight')"
        >
          TAILLIGHT
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
    </div>

    <!-- ═══════════════ NETLOG TAB ═══════════════ -->
    <template v-if="activeTab === 'netlog'">
      <!-- Chart 1: Total volume (stacked bar by host) -->
      <div>
        <h3 class="text-t-fg-dark mb-2 text-xs font-semibold uppercase tracking-wide">
          Total Volume
        </h3>
        <div class="bg-t-bg-dark border-t-border rounded border p-3">
          <VisXYContainer :data="netlogVolume.chartData" :height="220" :duration="0" :padding="{ top: 8, right: 8 }">
            <VisStackedBar
              :x="xTotal"
              :y="netlogHostYAccessors()"
              :color="netlogHostColorAccessor"
              :barPadding="0.6"
              :roundedCorners="2"
              :dataStep="dataStep"
            />
            <VisAxis type="x" :tickFormat="xTickFormat" :gridLine="false" :tickLine="false" />
            <VisAxis type="y" :gridLine="true" :tickLine="false" />
            <VisCrosshair :template="netlogHostTemplate" />
            <VisTooltip />
          </VisXYContainer>
        </div>

        <!-- Legend -->
        <div class="mt-2 flex flex-wrap gap-3">
          <span
            v-for="(host, i) in netlogVolume.hosts"
            :key="host"
            class="flex items-center gap-1 text-xs"
          >
            <span
              class="inline-block h-2.5 w-2.5 rounded-sm"
              :style="{ backgroundColor: accentColors[i % accentColors.length] }"
            />
            <span :style="{ color: accentColors[i % accentColors.length] }">{{ host }}</span>
          </span>
        </div>
      </div>

      <!-- Chart 2: Individual bar chart per host -->
      <div v-if="netlogVolume.chartData.length > 0" class="grid grid-cols-2 gap-4 lg:grid-cols-3">
        <div v-for="(host, idx) in netlogVolume.hosts" :key="host">
          <h3 class="text-t-fg-dark relative mb-1 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide">
            <span
              class="inline-block h-2 w-2 rounded-sm"
              :style="{ backgroundColor: accentColors[idx % accentColors.length] }"
            />
            {{ host }}
            <span
              v-if="hoveredHost[host]"
              class="pointer-events-none absolute inset-x-0 text-center font-normal normal-case tracking-normal"
            >
              <span class="text-t-fg-dark">{{ formatHoverTime(hoveredHost[host]!.x) }} - </span>
              <span
                class="font-bold"
                :style="{ color: accentColors[idx % accentColors.length] }"
              >{{ (hoveredHost[host]![host] as number) ?? 0 }}</span>
            </span>
          </h3>
          <div
            class="hide-tooltip bg-t-bg-dark border-t-border rounded border p-2"
            @mouseleave="hoveredHost[host] = null"
          >
            <VisXYContainer :data="netlogVolume.chartData" :height="120" :duration="0" :padding="{ top: 4, right: 4 }">
              <VisStackedBar
                :x="xTotal"
                :y="singleNetlogHostYAccessor(host)"
                :color="() => accentColors[idx % accentColors.length]"
                :barPadding="0.6"
                :roundedCorners="2"
                :dataStep="dataStep"
              />
              <VisAxis type="x" :tickFormat="xTickFormat" :numTicks="3" :gridLine="false" :tickLine="false" />
              <VisAxis type="y" :gridLine="true" :tickLine="false" />
              <VisCrosshair :template="singleNetlogHostTracker(host)" />
              <VisTooltip />
            </VisXYContainer>
          </div>
        </div>
      </div>
      <div v-else-if="!netlogVolume.loading && !netlogVolume.error"
        class="bg-t-bg-dark border-t-border text-t-fg-dark flex items-center justify-center rounded border py-16 text-sm">
        No data for the selected time range. Try a longer period.
      </div>
    </template>

    <!-- ═══════════════ SRVLOG TAB ═══════════════ -->
    <template v-if="activeTab === 'srvlog'">
      <!-- Chart 1: Total volume (stacked bar by host) -->
      <div>
        <h3 class="text-t-fg-dark mb-2 text-xs font-semibold uppercase tracking-wide">
          Total Volume
        </h3>
        <div class="bg-t-bg-dark border-t-border rounded border p-3">
          <VisXYContainer :data="srvlogVolume.chartData" :height="220" :duration="0" :padding="{ top: 8, right: 8 }">
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
            v-for="(host, i) in srvlogVolume.hosts"
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
      <div v-if="srvlogVolume.chartData.length > 0" class="grid grid-cols-2 gap-4 lg:grid-cols-3">
        <div v-for="(host, i) in srvlogVolume.hosts" :key="host">
          <h3 class="text-t-fg-dark relative mb-1 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide">
            <span
              class="inline-block h-2 w-2 rounded-sm"
              :style="{ backgroundColor: accentColors[i % accentColors.length] }"
            />
            {{ host }}
            <span
              v-if="hoveredHost[host]"
              class="pointer-events-none absolute inset-x-0 text-center font-normal normal-case tracking-normal"
            >
              <span class="text-t-fg-dark">{{ formatHoverTime(hoveredHost[host]!.x) }} - </span>
              <span
                class="font-bold"
                :style="{ color: accentColors[i % accentColors.length] }"
              >{{ (hoveredHost[host]![host] as number) ?? 0 }}</span>
            </span>
          </h3>
          <div
            class="hide-tooltip bg-t-bg-dark border-t-border rounded border p-2"
            @mouseleave="hoveredHost[host] = null"
          >
            <VisXYContainer :data="srvlogVolume.chartData" :height="120" :duration="0" :padding="{ top: 4, right: 4 }">
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
      <div v-else-if="!srvlogVolume.loading && !srvlogVolume.error"
        class="bg-t-bg-dark border-t-border text-t-fg-dark flex items-center justify-center rounded border py-16 text-sm">
        No data for the selected time range. Try a longer period.
      </div>
    </template>

    <!-- ═══════════════ APPLOG TAB ═══════════════ -->
    <template v-if="activeTab === 'applog'">
      <!-- Chart 1: Total volume (stacked bar by service) -->
      <div>
        <h3 class="text-t-fg-dark mb-2 text-xs font-semibold uppercase tracking-wide">
          Total Volume
        </h3>
        <div class="bg-t-bg-dark border-t-border rounded border p-3">
          <VisXYContainer :data="applogVolume.chartData" :height="220" :duration="0" :padding="{ top: 8, right: 8 }">
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
            v-for="(service, i) in applogVolume.services"
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
      <div v-if="applogVolume.chartData.length > 0" class="grid grid-cols-2 gap-4 lg:grid-cols-3">
        <div v-for="(service, i) in applogVolume.services" :key="service">
          <h3 class="text-t-fg-dark relative mb-1 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide">
            <span
              class="inline-block h-2 w-2 rounded-sm"
              :style="{ backgroundColor: accentColors[i % accentColors.length] }"
            />
            {{ service }}
            <span
              v-if="hoveredService[service]"
              class="pointer-events-none absolute inset-x-0 text-center font-normal normal-case tracking-normal"
            >
              <span class="text-t-fg-dark">{{ formatHoverTime(hoveredService[service]!.x) }} - </span>
              <span
                class="font-bold"
                :style="{ color: accentColors[i % accentColors.length] }"
              >{{ (hoveredService[service]![service] as number) ?? 0 }}</span>
            </span>
          </h3>
          <div
            class="hide-tooltip bg-t-bg-dark border-t-border rounded border p-2"
            @mouseleave="hoveredService[service] = null"
          >
            <VisXYContainer :data="applogVolume.chartData" :height="120" :duration="0" :padding="{ top: 4, right: 4 }">
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
      <div v-else-if="!applogVolume.loading && !applogVolume.error"
        class="bg-t-bg-dark border-t-border text-t-fg-dark flex items-center justify-center rounded border py-16 text-sm">
        No data for the selected time range. Try a longer period.
      </div>
    </template>

    <!-- ═══════════════ RSRVLOG TAB ═══════════════ -->
    <template v-if="activeTab === 'rsyslog'">
      <!-- KPI Cards -->
      <div class="relative">
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
            <template v-if="rsyslogStats.summary.total_failed > 0 || rsyslogStats.summary.total_suspended > 0">
              <div class="text-t-fg-dark text-[10px] font-semibold uppercase tracking-wider">Failures</div>
              <div class="text-t-red mt-1 text-xl font-bold">
                {{ formatCount(rsyslogStats.summary.total_failed) }}
              </div>
              <div class="text-t-fg-dark text-xs">
                {{ formatCount(rsyslogStats.summary.total_suspended) }} suspended
              </div>
            </template>
            <template v-else>
              <div class="text-t-fg-dark text-[10px] font-semibold uppercase tracking-wider">Output Health</div>
              <div class="text-t-green mt-1 text-xl font-bold">all ok</div>
            </template>
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
        <div
          v-else-if="!rsyslogTileLoading"
          class="bg-t-bg-dark border-t-border text-t-fg-dark flex items-center justify-center rounded border py-10 text-sm"
        >
          No stats available.
        </div>
        <div
          v-if="rsyslogTileLoading"
          class="bg-t-bg/60 pointer-events-none absolute inset-0 flex items-center justify-center rounded"
        >
          <LoadingIndicator />
        </div>
      </div>

      <!-- Received vs Written -->
      <div class="relative">
        <h3 class="text-t-fg-dark mb-2 text-xs font-semibold uppercase tracking-wide">Messages Over Time</h3>
        <div class="bg-t-bg-dark border-t-border rounded border p-3">
          <VisXYContainer :data="rsMessagesData" :height="220" :duration="0" :padding="{ top: 8, right: 8 }">
            <VisLine :x="rsMessagesX" :y="rsReceivedY" :color="accentColors[0]" :curveType="'monotoneX'" />
            <VisLine :x="rsMessagesX" :y="rsWrittenY" :color="accentColors[1]" :curveType="'monotoneX'" />
            <VisAxis type="x" :tickFormat="xTickFormat" :gridLine="false" :tickLine="false" />
            <VisAxis type="y" :gridLine="true" :tickLine="false" />
            <VisCrosshair :template="rsMessagesTooltip" />
            <VisTooltip />
          </VisXYContainer>
        </div>
        <div class="mt-2 flex gap-4">
          <span class="flex items-center gap-1 text-xs">
            <span class="inline-block h-2.5 w-2.5 rounded-sm" :style="{ backgroundColor: accentColors[0] }" />
            <span class="text-t-fg-dark">Received</span>
          </span>
          <span class="flex items-center gap-1 text-xs">
            <span class="inline-block h-2.5 w-2.5 rounded-sm" :style="{ backgroundColor: accentColors[1] }" />
            <span class="text-t-fg-dark">Written to DB</span>
          </span>
        </div>
        <div
          v-if="rsyslogTileLoading"
          class="bg-t-bg/60 pointer-events-none absolute inset-0 flex items-center justify-center rounded"
        >
          <LoadingIndicator />
        </div>
      </div>

      <!-- Queue Depth -->
      <div class="relative">
        <h3 class="text-t-fg-dark mb-2 text-xs font-semibold uppercase tracking-wide">Queue Depth</h3>
        <div class="bg-t-bg-dark border-t-border rounded border p-3">
          <VisXYContainer :data="rsyslogStats.queueLine" :height="160" :duration="0" :padding="{ top: 8, right: 8 }">
            <VisLine :x="lineX" :y="lineY" :color="accentColors[2]" :curveType="'monotoneX'" />
            <VisAxis type="x" :tickFormat="xTickFormat" :gridLine="false" :tickLine="false" />
            <VisAxis type="y" :gridLine="true" :tickLine="false" />
            <VisCrosshair :template="rsQueueTooltip" />
            <VisTooltip />
          </VisXYContainer>
        </div>
        <div
          v-if="rsyslogTileLoading"
          class="bg-t-bg/60 pointer-events-none absolute inset-0 flex items-center justify-center rounded"
        >
          <LoadingIndicator />
        </div>
      </div>

      <!-- Pipeline Overview -->
      <div class="relative">
        <div v-if="rsyslogStats.summary" class="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <!-- Inputs -->
          <div class="bg-t-bg-dark border-t-border rounded border">
            <h3 class="text-t-fg-dark border-t-border border-b px-3 py-2 text-xs font-semibold uppercase tracking-wide">Inputs</h3>
            <div v-for="inp in rsyslogInputs" :key="inp.name" class="border-t-border flex items-center justify-between border-b px-3 py-1.5 text-xs last:border-b-0 transition-colors hover:bg-white/[0.03]">
              <span class="text-t-fg">{{ inp.name }}</span>
              <span class="text-t-fg-dark font-mono">{{ formatCount(inp.received) }} <span class="opacity-50">received</span></span>
            </div>
            <div class="border-t-border flex items-center justify-between border-t px-3 py-1.5 text-xs transition-colors hover:bg-white/[0.03]">
              <span class="text-t-fg-dark font-semibold">Filtered</span>
              <span class="font-mono" :class="rsyslogFiltered > 0 ? 'text-t-yellow' : 'text-t-fg-dark'">{{ formatCount(rsyslogFiltered) }}</span>
            </div>
          </div>

          <!-- Outputs -->
          <div class="bg-t-bg-dark border-t-border rounded border">
            <h3 class="text-t-fg-dark border-t-border border-b px-3 py-2 text-xs font-semibold uppercase tracking-wide">Outputs</h3>
            <div v-for="out in rsyslogOutputs" :key="out.name" class="border-t-border flex items-center justify-between border-b px-3 py-1.5 text-xs last:border-b-0 transition-colors hover:bg-white/[0.03]">
              <span class="text-t-fg">{{ out.name }}</span>
              <span class="text-t-fg-dark font-mono">
                {{ formatCount(out.processed) }} <span class="opacity-50">ok</span>
                <template v-if="out.failed > 0">
                  &middot; <span class="text-t-red">{{ formatCount(out.failed) }} failed</span>
                </template>
                <template v-if="out.suspended > 0">
                  &middot; <span class="text-t-yellow">{{ formatCount(out.suspended) }} suspended</span>
                </template>
              </span>
            </div>
          </div>
        </div>
        <div
          v-if="rsyslogTileLoading"
          class="bg-t-bg/60 pointer-events-none absolute inset-0 flex items-center justify-center rounded"
        >
          <LoadingIndicator />
        </div>
      </div>
    </template>

    <!-- ═══════════════ TAILLIGHT TAB ═══════════════ -->
    <template v-if="activeTab === 'taillight'">
      <!-- KPI Cards -->
      <div v-if="taillightMetrics.summary" class="grid grid-cols-2 gap-3 lg:grid-cols-4">
        <div class="bg-t-bg-dark border-t-border rounded border p-3">
          <div class="text-t-fg-dark text-[10px] font-semibold uppercase tracking-wider">SSE Clients</div>
          <div class="text-t-fg mt-1 text-xl font-bold">
            {{ taillightMetrics.summary.sse_clients_srvlog + taillightMetrics.summary.sse_clients_applog }}
          </div>
          <div class="text-t-fg-dark text-xs">
            {{ taillightMetrics.summary.sse_clients_srvlog }} srvlog &middot;
            {{ taillightMetrics.summary.sse_clients_applog }} applog
          </div>
        </div>
        <div class="bg-t-bg-dark border-t-border rounded border p-3">
          <div class="text-t-fg-dark text-[10px] font-semibold uppercase tracking-wider">DB Pool</div>
          <div class="text-t-fg mt-1 text-xl font-bold">
            {{ taillightMetrics.summary.db_pool_active }} / {{ taillightMetrics.summary.db_pool_total }}
          </div>
          <div class="text-t-fg-dark text-xs">
            active / total &middot; {{ taillightMetrics.summary.db_pool_idle }} idle
          </div>
        </div>
        <div class="bg-t-bg-dark border-t-border rounded border p-3">
          <div class="text-t-fg-dark text-[10px] font-semibold uppercase tracking-wider">Events Rate</div>
          <div class="text-t-fg mt-1 text-xl font-bold">{{ formatRate(taillightMetrics.summary.events_rate) }}</div>
          <div class="text-t-fg-dark text-xs">
            broadcast/min
            <template v-if="taillightMetrics.summary.events_dropped > 0">
              &middot; <span class="text-t-red">{{ formatCount(taillightMetrics.summary.events_dropped) }} dropped</span>
            </template>
          </div>
        </div>
        <div class="bg-t-bg-dark border-t-border rounded border p-3">
          <div class="text-t-fg-dark text-[10px] font-semibold uppercase tracking-wider">Ingest Rate</div>
          <div class="text-t-fg mt-1 text-xl font-bold">{{ formatRate(taillightMetrics.summary.ingest_rate) }}</div>
          <div class="text-t-fg-dark text-xs">
            applog/min
            <template v-if="taillightMetrics.summary.applog_ingest_errors > 0">
              &middot; <span class="text-t-red">{{ formatCount(taillightMetrics.summary.applog_ingest_errors) }} errors</span>
            </template>
          </div>
        </div>
      </div>

      <!-- Health Warnings -->
      <div
        v-if="taillightMetrics.summary && (taillightMetrics.summary.events_dropped > 0 || taillightMetrics.summary.applog_ingest_errors > 0 || taillightMetrics.summary.listener_reconnects > 0)"
        class="bg-t-bg-dark border-t-border rounded border p-3"
      >
        <div class="text-t-fg-dark text-[10px] font-semibold uppercase tracking-wider mb-2">Warnings</div>
        <div class="flex flex-wrap gap-3 text-xs">
          <span v-if="taillightMetrics.summary.events_dropped > 0" class="text-t-yellow">
            {{ formatCount(taillightMetrics.summary.events_dropped) }} srvlog events dropped (slow SSE clients)
          </span>
          <span v-if="taillightMetrics.summary.applog_events_dropped > 0" class="text-t-yellow">
            {{ formatCount(taillightMetrics.summary.applog_events_dropped) }} applog events dropped (slow SSE clients)
          </span>
          <span v-if="taillightMetrics.summary.applog_ingest_errors > 0" class="text-t-red">
            {{ formatCount(taillightMetrics.summary.applog_ingest_errors) }} applog ingest errors
          </span>
          <span v-if="taillightMetrics.summary.listener_reconnects > 0" class="text-t-yellow">
            {{ formatCount(taillightMetrics.summary.listener_reconnects) }} listener reconnects
          </span>
        </div>
      </div>

      <!-- Events Over Time -->
      <div>
        <h3 class="text-t-fg-dark mb-2 text-xs font-semibold uppercase tracking-wide">Events Over Time</h3>
        <div class="bg-t-bg-dark border-t-border rounded border p-3">
          <VisXYContainer :data="tlEventsData" :height="220" :duration="0" :padding="{ top: 8, right: 8 }">
            <VisLine :x="tlDualX" :y="tlSrvlogY" :color="accentColors[0]" :curveType="'monotoneX'" :lineWidth="2" />
            <VisLine :x="tlDualX" :y="tlApplogY" :color="accentColors[1]" :curveType="'monotoneX'" :lineWidth="2" />
            <VisAxis type="x" :tickFormat="xTickFormat" :gridLine="false" :tickLine="false" />
            <VisAxis type="y" :gridLine="true" :tickLine="false" />
            <VisCrosshair :template="tlEventsTooltip" />
            <VisTooltip />
          </VisXYContainer>
        </div>
        <div class="mt-2 flex gap-4">
          <span class="flex items-center gap-1 text-xs">
            <span class="inline-block h-2.5 w-2.5 rounded-sm" :style="{ backgroundColor: accentColors[0] }" />
            <span class="text-t-fg-dark">Srvlog Broadcast</span>
          </span>
          <span class="flex items-center gap-1 text-xs">
            <span class="inline-block h-2.5 w-2.5 rounded-sm" :style="{ backgroundColor: accentColors[1] }" />
            <span class="text-t-fg-dark">Applog Broadcast</span>
          </span>
        </div>
      </div>

      <!-- SSE Clients Over Time -->
      <div>
        <h3 class="text-t-fg-dark mb-2 text-xs font-semibold uppercase tracking-wide">SSE Clients Over Time</h3>
        <div class="bg-t-bg-dark border-t-border rounded border p-3">
          <VisXYContainer :data="tlSseData" :height="160" :duration="0" :padding="{ top: 8, right: 8 }">
            <VisLine :x="tlDualX" :y="tlSrvlogY" :color="accentColors[0]" :curveType="'monotoneX'" :lineWidth="2" />
            <VisLine :x="tlDualX" :y="tlApplogY" :color="accentColors[1]" :curveType="'monotoneX'" :lineWidth="2" />
            <VisAxis type="x" :tickFormat="xTickFormat" :gridLine="false" :tickLine="false" />
            <VisAxis type="y" :gridLine="true" :tickLine="false" />
            <VisCrosshair :template="tlSseTooltip" />
            <VisTooltip />
          </VisXYContainer>
        </div>
        <div class="mt-2 flex gap-4">
          <span class="flex items-center gap-1 text-xs">
            <span class="inline-block h-2.5 w-2.5 rounded-sm" :style="{ backgroundColor: accentColors[0] }" />
            <span class="text-t-fg-dark">Srvlog Clients</span>
          </span>
          <span class="flex items-center gap-1 text-xs">
            <span class="inline-block h-2.5 w-2.5 rounded-sm" :style="{ backgroundColor: accentColors[1] }" />
            <span class="text-t-fg-dark">Applog Clients</span>
          </span>
        </div>
      </div>

      <!-- DB Pool Over Time -->
      <div>
        <h3 class="text-t-fg-dark mb-2 text-xs font-semibold uppercase tracking-wide">DB Pool Over Time</h3>
        <div class="bg-t-bg-dark border-t-border rounded border p-3">
          <VisXYContainer :data="tlPoolData" :height="160" :duration="0" :padding="{ top: 8, right: 8 }">
            <VisLine :x="tlPoolX" :y="tlActiveY" :color="accentColors[0]" :curveType="'monotoneX'" />
            <VisLine :x="tlPoolX" :y="tlIdleY" :color="accentColors[1]" :curveType="'monotoneX'" />
            <VisLine :x="tlPoolX" :y="tlTotalY" :color="accentColors[2]" :curveType="'monotoneX'" />
            <VisAxis type="x" :tickFormat="xTickFormat" :gridLine="false" :tickLine="false" />
            <VisAxis type="y" :gridLine="true" :tickLine="false" />
            <VisCrosshair :template="tlPoolTooltip" />
            <VisTooltip />
          </VisXYContainer>
        </div>
        <div class="mt-2 flex gap-4">
          <span class="flex items-center gap-1 text-xs">
            <span class="inline-block h-2.5 w-2.5 rounded-sm" :style="{ backgroundColor: accentColors[0] }" />
            <span class="text-t-fg-dark">Active</span>
          </span>
          <span class="flex items-center gap-1 text-xs">
            <span class="inline-block h-2.5 w-2.5 rounded-sm" :style="{ backgroundColor: accentColors[1] }" />
            <span class="text-t-fg-dark">Idle</span>
          </span>
          <span class="flex items-center gap-1 text-xs">
            <span class="inline-block h-2.5 w-2.5 rounded-sm" :style="{ backgroundColor: accentColors[2] }" />
            <span class="text-t-fg-dark">Total</span>
          </span>
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
