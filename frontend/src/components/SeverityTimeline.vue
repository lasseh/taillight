<script setup lang="ts">
import { computed } from 'vue'
import { VisXYContainer, VisStackedBar, VisAxis, VisCrosshair, VisTooltip } from '@unovis/vue'
import { getCSSColor } from '@/lib/constants'
import type { SeverityVolumeBucket } from '@/types/stats'

interface SeverityDataRecord {
  x: number
  [category: string]: number
}

const props = defineProps<{
  data: SeverityVolumeBucket[]
  colorMap: Record<string, string>
  categories: string[]
}>()

const chartData = computed<SeverityDataRecord[]>(() => {
  return props.data.map((b) => {
    const rec: SeverityDataRecord = { x: new Date(b.time).getTime() }
    for (const cat of props.categories) {
      rec[cat] = b.by_severity[cat] ?? 0
    }
    return rec
  })
})

const xAccessor = (d: SeverityDataRecord) => d.x

const yAccessors = computed(() =>
  props.categories.map((cat) => (d: SeverityDataRecord) => (d[cat] as number) ?? 0),
)

const colorAccessor = computed(() => {
  const resolved = props.categories.map((cat) => {
    const varName = props.colorMap[cat]
    return varName ? getCSSColor(varName) : '#888'
  })
  return (_d: SeverityDataRecord, i: number) => resolved[i % resolved.length]
})

function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

const tooltipTemplate = computed(() => {
  const cats = props.categories
  const cMap = props.colorMap
  return (d: SeverityDataRecord) => {
    const date = new Date(d.x)
    const lines = [...cats]
      .reverse()
      .map((cat) => {
        const v = (d[cat] as number) ?? 0
        if (v === 0) return ''
        const color = getCSSColor(cMap[cat] ?? '')
        return `<div><span style="color:${color}">●</span> ${escapeHtml(cat)}: <b>${v}</b></div>`
      })
      .filter(Boolean)
      .join('')
    return `<div style="font-family:var(--font-mono);font-size:11px;padding:4px 8px">
      <div style="color:var(--color-t-fg-dark)">${date.toLocaleString()}</div>
      ${lines}
    </div>`
  }
})

const xTickFormat = (v: number) => {
  const d = new Date(v)
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', hour12: false })
}
</script>

<template>
  <div>
    <VisXYContainer :data="chartData" :height="140" :duration="0" :padding="{ top: 8, right: 8 }">
      <VisStackedBar
        :x="xAccessor"
        :y="yAccessors"
        :color="colorAccessor"
        :barPadding="0.6"
        :roundedCorners="2"
        :dataStep="900_000"
      />
      <VisAxis type="x" :tickFormat="xTickFormat" :gridLine="false" :tickLine="false" />
      <VisAxis type="y" :gridLine="true" :tickLine="false" />
      <VisCrosshair :template="tooltipTemplate" />
      <VisTooltip />
    </VisXYContainer>

    <!-- Legend -->
    <div class="mt-2 flex flex-wrap gap-3">
      <span
        v-for="cat in [...categories].reverse()"
        :key="cat"
        class="flex items-center gap-1 text-xs"
      >
        <span
          class="inline-block h-2.5 w-2.5 rounded-sm"
          :style="{ backgroundColor: getCSSColor(colorMap[cat] ?? '') }"
        />
        <span class="text-t-fg-dark">{{ cat }}</span>
      </span>
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
