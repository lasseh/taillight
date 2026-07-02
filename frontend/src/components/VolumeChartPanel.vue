<script setup lang="ts">
import { ref, computed } from 'vue'
import { VisXYContainer, VisStackedBar, VisAxis, VisCrosshair, VisTooltip } from '@unovis/vue'
import { useTheme } from '@/composables/useTheme'
import type { VolumeDataRecord } from '@/types/stats'

// Volume panel for a single feed tab: total stacked bar + legend, then a
// per-key (host/service) small-multiples grid with hover readouts.
const props = defineProps<{
  chartData: VolumeDataRecord[]
  keys: string[]
  dataStep: number
  xTickFormat: (v: number) => string
  formatHoverTime: (ts: number) => string
  loading: boolean
  error: string | null
}>()

const { current: theme } = useTheme()
const accentColors = computed(() => theme.value.chartColors)

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

// X accessor
const x = (d: VolumeDataRecord) => d.x

const yAccessors = computed(() =>
  props.keys.map((k) => (d: VolumeDataRecord) => (d[k] as number) ?? 0),
)

function colorAccessor(_d: VolumeDataRecord, i: number) {
  return accentColors.value[i % accentColors.value.length]
}

function crosshairTemplate(d: VolumeDataRecord) {
  const date = new Date(d.x)
  const lines = props.keys
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

// --- Hover state for individual charts ---
const hovered = ref<Record<string, VolumeDataRecord | null>>({})

function singleYAccessor(key: string) {
  return [(d: VolumeDataRecord) => (d[key] as number) ?? 0]
}

function singleTracker(key: string) {
  return (d: VolumeDataRecord) => {
    hovered.value[key] = d
    return ''
  }
}
</script>

<template>
  <!-- Chart 1: Total volume (stacked bar by key) -->
  <div>
    <h3 class="text-t-fg-dark mb-2 text-xs font-semibold uppercase tracking-wide">Total Volume</h3>
    <div class="bg-t-bg-dark border-t-border rounded border p-3">
      <VisXYContainer :data="chartData" :height="220" :duration="0" :padding="{ top: 8, right: 8 }">
        <VisStackedBar
          :x="x"
          :y="yAccessors"
          :color="colorAccessor"
          :barPadding="0.6"
          :roundedCorners="2"
          :dataStep="dataStep"
        />
        <VisAxis type="x" :tickFormat="xTickFormat" :gridLine="false" :tickLine="false" />
        <VisAxis type="y" :gridLine="true" :tickLine="false" />
        <VisCrosshair :template="crosshairTemplate" />
        <VisTooltip />
      </VisXYContainer>
    </div>

    <!-- Legend -->
    <div class="mt-2 flex flex-wrap gap-3">
      <span v-for="(name, i) in keys" :key="name" class="flex items-center gap-1 text-xs">
        <span
          class="inline-block h-2.5 w-2.5 rounded-sm"
          :style="{ backgroundColor: accentColors[i % accentColors.length] }"
        />
        <span :style="{ color: accentColors[i % accentColors.length] }">{{ name }}</span>
      </span>
    </div>
  </div>

  <!-- Chart 2: Individual bar chart per key -->
  <div v-if="chartData.length > 0" class="grid grid-cols-2 gap-4 lg:grid-cols-3">
    <div v-for="(name, i) in keys" :key="name">
      <h3
        class="text-t-fg-dark relative mb-1 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide"
      >
        <span
          class="inline-block h-2 w-2 rounded-sm"
          :style="{ backgroundColor: accentColors[i % accentColors.length] }"
        />
        {{ name }}
        <span
          v-if="hovered[name]"
          class="pointer-events-none absolute inset-x-0 text-center font-normal normal-case tracking-normal"
        >
          <span class="text-t-fg-dark">{{ formatHoverTime(hovered[name]!.x) }} - </span>
          <span class="font-bold" :style="{ color: accentColors[i % accentColors.length] }">{{
            hovered[name]![name] ?? 0
          }}</span>
        </span>
      </h3>
      <div
        class="hide-tooltip bg-t-bg-dark border-t-border rounded border p-2"
        @mouseleave="hovered[name] = null"
      >
        <VisXYContainer
          :data="chartData"
          :height="120"
          :duration="0"
          :padding="{ top: 4, right: 4 }"
        >
          <VisStackedBar
            :x="x"
            :y="singleYAccessor(name)"
            :color="() => accentColors[i % accentColors.length]"
            :barPadding="0.6"
            :roundedCorners="2"
            :dataStep="dataStep"
          />
          <VisAxis
            type="x"
            :tickFormat="xTickFormat"
            :numTicks="3"
            :gridLine="false"
            :tickLine="false"
          />
          <VisAxis type="y" :gridLine="true" :tickLine="false" />
          <VisCrosshair :template="singleTracker(name)" />
          <VisTooltip />
        </VisXYContainer>
      </div>
    </div>
  </div>
  <div
    v-else-if="!loading && !error"
    class="bg-t-bg-dark border-t-border text-t-fg-dark flex items-center justify-center rounded border py-16 text-sm"
  >
    No data for the selected time range. Try a longer period.
  </div>
</template>

<style scoped>
:deep(.unovis-xy-container) svg text {
  fill: var(--color-t-fg-dark);
}

:deep(.unovis-xy-container) path[class*='-bar'] {
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
