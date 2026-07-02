<script setup lang="ts">
import { ref, computed } from 'vue'
import { VisXYContainer, VisStackedBar, VisAxis, VisCrosshair, VisTooltip } from '@unovis/vue'
import type { ActivityBucket } from '@/types/device'
import { useTheme } from '@/composables/useTheme'
import { formatNumber } from '@/lib/format'

const props = defineProps<{
  items: ActivityBucket[]
}>()

const { current: theme } = useTheme()
const accentColors = computed(() => theme.value.chartColors)

interface ActivityRecord {
  x: number
  count: number
}
const chartData = computed<ActivityRecord[]>(() =>
  props.items.map((b) => ({ x: new Date(b.time).getTime(), count: b.count })),
)
const xAccessor = (d: ActivityRecord) => d.x
const yAccessors = [(d: ActivityRecord) => d.count]
const hovered = ref<ActivityRecord | null>(null)
function tracker(d: ActivityRecord) {
  hovered.value = d
  return ''
}
const tickFormat = (v: number) =>
  new Date(v).toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', hour12: false })
</script>

<template>
  <div v-if="chartData.length > 0" class="border-t-border mt-3 border-t pt-3">
    <h4
      class="text-t-fg-dark relative mb-1 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide"
    >
      Log Activity
      <span class="text-t-fg-dark/70 font-normal normal-case">(24h)</span>
      <span
        v-if="hovered"
        class="pointer-events-none absolute inset-x-0 text-center font-normal normal-case tracking-normal"
      >
        <span class="text-t-fg-dark">{{ tickFormat(hovered.x) }} - </span>
        <span class="font-bold" :style="{ color: accentColors[0] }">{{
          formatNumber(hovered.count)
        }}</span>
      </span>
    </h4>
    <div class="hide-tooltip" @mouseleave="hovered = null">
      <VisXYContainer :data="chartData" :height="120" :duration="0" :padding="{ top: 4, right: 4 }">
        <VisStackedBar
          :x="xAccessor"
          :y="yAccessors"
          :color="() => accentColors[0]"
          :barPadding="0.6"
          :roundedCorners="2"
          :dataStep="900_000"
        />
        <VisAxis
          type="x"
          :tickFormat="tickFormat"
          :numTicks="3"
          :gridLine="false"
          :tickLine="false"
        />
        <VisAxis type="y" :gridLine="true" :tickLine="false" />
        <VisCrosshair :template="tracker" />
        <VisTooltip />
      </VisXYContainer>
    </div>
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
