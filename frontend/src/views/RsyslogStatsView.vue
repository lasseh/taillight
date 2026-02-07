<script setup lang="ts">
import { computed, onMounted, onUnmounted } from 'vue'
import { VisXYContainer, VisStackedBar, VisAxis, VisCrosshair, VisTooltip } from '@unovis/vue'
import { useRsyslogStatsStore } from '@/stores/rsyslog-stats'
import { useTheme } from '@/composables/useTheme'
import type { RsyslogStatsDataRecord } from '@/types/rsyslog-stats'

const store = useRsyslogStatsStore()
const { current: theme } = useTheme()

const accentColors = computed(() => theme.value.chartColors)

const presets: { label: string; range: string; interval: string }[] = [
  { label: '1h', range: '1h', interval: '1m' },
  { label: '6h', range: '6h', interval: '5m' },
  { label: '24h', range: '24h', interval: '15m' },
  { label: '7d', range: '7d', interval: '1h' },
]

const intervalMs: Record<string, number> = {
  '1m': 60_000,
  '5m': 300_000,
  '15m': 900_000,
  '1h': 3_600_000,
}

const dataStep = computed(() => intervalMs[store.interval] ?? 60_000)

function selectPreset(range: string, interval: string) {
  store.setPreset(range, interval)
}

function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;').replace(/'/g, '&#39;')
}

// X accessor
const xAccessor = (d: RsyslogStatsDataRecord) => d.x

// Chart helpers
function makeYAccessors(keys: string[]) {
  return keys.map((k) => (d: RsyslogStatsDataRecord) => (d[k] as number) ?? 0)
}

function makeColorAccessor(_d: RsyslogStatsDataRecord, i: number) {
  return accentColors.value[i % accentColors.value.length]
}

function makeTemplate(keys: string[]) {
  return (d: RsyslogStatsDataRecord) => {
    const date = new Date(d.x)
    const lines = keys
      .map((k, i) => {
        const v = (d[k] as number) ?? 0
        if (v === 0) return ''
        const color = accentColors.value[i % accentColors.value.length]
        return `<div><span style="color:${color}">&#9679;</span> ${escapeHtml(k)}: <b>${v}</b></div>`
      })
      .filter(Boolean)
      .join('')
    return `<div style="font-family:var(--font-mono);font-size:11px;padding:4px 8px">
      <div style="color:var(--color-t-fg-dark)">${date.toLocaleString()}</div>
      ${lines}
    </div>`
  }
}

const xTickFormat = (v: number) => {
  const d = new Date(v)
  const r = store.range
  if (r === '7d' || r === '30d') {
    return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
  }
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', hour12: false })
}

// KPI formatting
function formatRate(v: number): string {
  return v.toFixed(1)
}

function formatCount(v: number): string {
  if (v >= 1_000_000) return (v / 1_000_000).toFixed(1) + 'M'
  if (v >= 1_000) return (v / 1_000).toFixed(1) + 'K'
  return String(v)
}

onMounted(() => store.startRefresh())
onUnmounted(() => store.stopRefresh())
</script>

<template>
  <div class="flex flex-1 flex-col gap-6 overflow-y-auto p-4">
    <!-- Header + Preset buttons -->
    <div class="flex items-center gap-4">
      <span class="text-t-orange text-xs font-semibold uppercase tracking-wide">Rsyslog Stats</span>

      <span class="text-t-border">|</span>

      <span class="text-t-fg-dark text-xs">Range:</span>
      <button
        v-for="p in presets"
        :key="p.label"
        class="px-2 py-0.5 text-xs transition-colors"
        :class="
          store.range === p.range
            ? 'bg-t-bg-highlight text-t-orange'
            : 'text-t-fg-dark hover:text-t-fg'
        "
        @click="selectPreset(p.range, p.interval)"
      >
        {{ p.label }}
      </button>
      <span v-if="store.loading" class="text-t-fg-dark ml-2 text-xs">loading...</span>
      <span v-if="store.error" class="text-t-red ml-2 text-xs">{{ store.error }}</span>
      <span class="text-t-fg-dark ml-auto text-xs opacity-50">auto-refresh: 60s</span>
    </div>

    <!-- KPI Cards -->
    <div v-if="store.summary" class="grid grid-cols-2 gap-3 lg:grid-cols-4">
      <div class="bg-t-bg-dark border-t-border rounded border p-3">
        <div class="text-t-fg-dark text-[10px] font-semibold uppercase tracking-wider">Ingest Rate</div>
        <div class="text-t-fg mt-1 text-xl font-bold">{{ formatRate(store.summary.ingest_rate) }}</div>
        <div class="text-t-fg-dark text-xs">msgs/min</div>
      </div>
      <div class="bg-t-bg-dark border-t-border rounded border p-3">
        <div class="text-t-fg-dark text-[10px] font-semibold uppercase tracking-wider">Filter Rate</div>
        <div class="text-t-fg mt-1 text-xl font-bold">{{ formatRate(store.summary.filter_rate) }}%</div>
        <div class="text-t-fg-dark text-xs">submitted not processed</div>
      </div>
      <div class="bg-t-bg-dark border-t-border rounded border p-3">
        <div class="text-t-fg-dark text-[10px] font-semibold uppercase tracking-wider">Failures</div>
        <div class="mt-1 text-xl font-bold" :class="store.summary.total_failed > 0 ? 'text-t-red' : 'text-t-fg'">
          {{ formatCount(store.summary.total_failed) }}
        </div>
        <div class="text-t-fg-dark text-xs">
          {{ formatCount(store.summary.total_suspended) }} suspended
        </div>
      </div>
      <div class="bg-t-bg-dark border-t-border rounded border p-3">
        <div class="text-t-fg-dark text-[10px] font-semibold uppercase tracking-wider">Queue Health</div>
        <div class="text-t-fg mt-1 text-xl font-bold">{{ formatCount(store.summary.main_queue_size) }}</div>
        <div class="text-t-fg-dark text-xs">
          max {{ formatCount(store.summary.main_queue_max_size) }}
          <template v-if="store.summary.total_discarded > 0">
            &middot; <span class="text-t-red">{{ formatCount(store.summary.total_discarded) }} discarded</span>
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
          <VisXYContainer :data="store.ingestChartData" :height="200" :padding="{ top: 8, right: 8 }">
            <VisStackedBar
              :x="xAccessor"
              :y="makeYAccessors(store.ingestNames)"
              :color="makeColorAccessor"
              :barPadding="0.6"
              :roundedCorners="2"
              :dataStep="dataStep"
            />
            <VisAxis type="x" :tickFormat="xTickFormat" :gridLine="false" :tickLine="false" />
            <VisAxis type="y" :gridLine="true" :tickLine="false" />
            <VisCrosshair :template="makeTemplate(store.ingestNames)" />
            <VisTooltip />
          </VisXYContainer>
        </div>
        <div class="mt-2 flex flex-wrap gap-3">
          <span v-for="(name, i) in store.ingestNames" :key="name" class="flex items-center gap-1 text-xs">
            <span class="inline-block h-2.5 w-2.5 rounded-sm" :style="{ backgroundColor: accentColors[i % accentColors.length] }" />
            <span class="text-t-fg-dark">{{ name }}</span>
          </span>
        </div>
      </div>

      <!-- Queue Depth -->
      <div>
        <h3 class="text-t-fg-dark mb-2 text-xs font-semibold uppercase tracking-wide">Queue Depth</h3>
        <div class="bg-t-bg-dark border-t-border rounded border p-3">
          <VisXYContainer :data="store.queueChartData" :height="200" :padding="{ top: 8, right: 8 }">
            <VisStackedBar
              :x="xAccessor"
              :y="makeYAccessors(store.queueNames)"
              :color="makeColorAccessor"
              :barPadding="0.6"
              :roundedCorners="2"
              :dataStep="dataStep"
            />
            <VisAxis type="x" :tickFormat="xTickFormat" :gridLine="false" :tickLine="false" />
            <VisAxis type="y" :gridLine="true" :tickLine="false" />
            <VisCrosshair :template="makeTemplate(store.queueNames)" />
            <VisTooltip />
          </VisXYContainer>
        </div>
        <div class="mt-2 flex flex-wrap gap-3">
          <span v-for="(name, i) in store.queueNames" :key="name" class="flex items-center gap-1 text-xs">
            <span class="inline-block h-2.5 w-2.5 rounded-sm" :style="{ backgroundColor: accentColors[i % accentColors.length] }" />
            <span class="text-t-fg-dark">{{ name }}</span>
          </span>
        </div>
      </div>

      <!-- Action Throughput -->
      <div>
        <h3 class="text-t-fg-dark mb-2 text-xs font-semibold uppercase tracking-wide">Action Throughput</h3>
        <div class="bg-t-bg-dark border-t-border rounded border p-3">
          <VisXYContainer :data="store.processedChartData" :height="200" :padding="{ top: 8, right: 8 }">
            <VisStackedBar
              :x="xAccessor"
              :y="makeYAccessors(store.processedNames)"
              :color="makeColorAccessor"
              :barPadding="0.6"
              :roundedCorners="2"
              :dataStep="dataStep"
            />
            <VisAxis type="x" :tickFormat="xTickFormat" :gridLine="false" :tickLine="false" />
            <VisAxis type="y" :gridLine="true" :tickLine="false" />
            <VisCrosshair :template="makeTemplate(store.processedNames)" />
            <VisTooltip />
          </VisXYContainer>
        </div>
        <div class="mt-2 flex flex-wrap gap-3">
          <span v-for="(name, i) in store.processedNames" :key="name" class="flex items-center gap-1 text-xs">
            <span class="inline-block h-2.5 w-2.5 rounded-sm" :style="{ backgroundColor: accentColors[i % accentColors.length] }" />
            <span class="text-t-fg-dark">{{ name }}</span>
          </span>
        </div>
      </div>

      <!-- Failures & Suspensions -->
      <div>
        <h3 class="text-t-fg-dark mb-2 text-xs font-semibold uppercase tracking-wide">Failures &amp; Suspensions</h3>
        <div class="bg-t-bg-dark border-t-border rounded border p-3">
          <VisXYContainer :data="store.failedChartData" :height="200" :padding="{ top: 8, right: 8 }">
            <VisStackedBar
              :x="xAccessor"
              :y="makeYAccessors(store.failedNames)"
              :color="makeColorAccessor"
              :barPadding="0.6"
              :roundedCorners="2"
              :dataStep="dataStep"
            />
            <VisAxis type="x" :tickFormat="xTickFormat" :gridLine="false" :tickLine="false" />
            <VisAxis type="y" :gridLine="true" :tickLine="false" />
            <VisCrosshair :template="makeTemplate(store.failedNames)" />
            <VisTooltip />
          </VisXYContainer>
        </div>
        <div class="mt-2 flex flex-wrap gap-3">
          <span v-for="(name, i) in store.failedNames" :key="name" class="flex items-center gap-1 text-xs">
            <span class="inline-block h-2.5 w-2.5 rounded-sm" :style="{ backgroundColor: accentColors[i % accentColors.length] }" />
            <span class="text-t-fg-dark">{{ name }}</span>
          </span>
        </div>
      </div>
    </div>

    <!-- Component Table -->
    <div v-if="store.summary && store.summary.components.length > 0">
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
              v-for="(comp, idx) in store.summary.components"
              :key="`${comp.origin}-${comp.name}`"
              class="border-t-border transition-colors hover:bg-white/[0.02]"
              :class="idx < store.summary.components.length - 1 ? 'border-b' : ''"
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
</style>
