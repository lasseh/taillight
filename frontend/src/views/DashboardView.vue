<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { VisXYContainer, VisStackedBar, VisAxis, VisCrosshair, VisTooltip } from '@unovis/vue'
import { useDashboardStore } from '@/stores/dashboard'
import { useAppLogDashboardStore } from '@/stores/applog-dashboard'
import { useTheme } from '@/composables/useTheme'
import type { VolumeDataRecord } from '@/types/stats'

const route = useRoute()
const router = useRouter()
const dashboard = useDashboardStore()
const applogDashboard = useAppLogDashboardStore()
const { current: theme } = useTheme()

function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;')
}

type Tab = 'syslog' | 'applog'
const activeTab = ref<Tab>((route.query.tab as Tab) || 'syslog')

const accentColors = computed(() => theme.value.chartColors)

const presets: { label: string; range: string; interval: string }[] = [
  { label: '1h', range: '1h', interval: '1m' },
  { label: '6h', range: '6h', interval: '5m' },
  { label: '24h', range: '24h', interval: '15m' },
  { label: '7d', range: '7d', interval: '30m' },
  { label: '30d', range: '30d', interval: '6h' },
]

const intervalMs: Record<string, number> = {
  '1m': 60_000,
  '5m': 300_000,
  '15m': 900_000,
  '30m': 1_800_000,
  '6h': 21_600_000,
}

const dataStep = computed(() => intervalMs[activeTab.value === 'syslog' ? dashboard.interval : applogDashboard.interval] ?? 60_000)

const activeRange = computed(() => activeTab.value === 'syslog' ? dashboard.range : applogDashboard.range)
const activeLoading = computed(() => activeTab.value === 'syslog' ? dashboard.loading : applogDashboard.loading)
const activeError = computed(() => activeTab.value === 'syslog' ? dashboard.error : applogDashboard.error)

function switchTab(tab: Tab) {
  activeTab.value = tab
  router.replace({ query: { ...route.query, tab } })
  // Fetch if not already loaded
  if (tab === 'syslog' && dashboard.buckets?.length === 0) {
    dashboard.fetchVolume()
  } else if (tab === 'applog' && applogDashboard.buckets?.length === 0) {
    applogDashboard.fetchVolume()
  }
}

function selectPreset(range: string, interval: string) {
  if (activeTab.value === 'syslog') {
    dashboard.setPreset(range, interval)
  } else {
    applogDashboard.setPreset(range, interval)
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

  const r = route.query.range as string | undefined
  const preset = r ? presets.find((p) => p.range === r) : undefined

  // Always init the active tab's store
  const store = activeTab.value === 'syslog' ? dashboard : applogDashboard
  if (preset) {
    store.setPreset(preset.range, preset.interval)
  } else {
    store.fetchVolume()
  }
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
      </div>

      <span class="text-t-border">|</span>

      <span class="text-t-fg-dark text-xs">Range:</span>
      <button
        v-for="p in presets"
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
      <span v-if="activeLoading" class="text-t-fg-dark ml-2 text-xs">loading…</span>
      <span v-if="activeError" class="text-t-red ml-2 text-xs">{{ activeError }}</span>
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
