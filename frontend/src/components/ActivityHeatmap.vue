<script setup lang="ts">
import { computed, ref } from 'vue'

const props = withDefaults(defineProps<{
  /** Map of "YYYY-MM-DD HH" to count (24h format, e.g. "2025-02-19 14") */
  data: Record<string, number>
  /** CSS color variable name for the heatmap accent */
  colorVar?: string
  /** Label shown in tooltip */
  label?: string
}>(), {
  colorVar: '--color-t-teal',
  label: 'events',
})

// ── Build 7-day x 24-hour grid ──

interface Cell {
  key: string    // "YYYY-MM-DD HH"
  dayLabel: string
  dateLabel: string
  hour: number
  count: number
  level: number  // 0-4
}

const DAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']
const DAY_MS = 86_400_000

const days = computed(() => {
  const today = new Date()
  today.setHours(0, 0, 0, 0)
  const result: { iso: string; dayLabel: string; dateLabel: string }[] = []
  for (let i = 6; i >= 0; i--) {
    const d = new Date(today.getTime() - i * DAY_MS)
    const iso = formatDate(d)
    result.push({
      iso,
      dayLabel: i === 0 ? 'Today' : i === 1 ? 'Yest' : DAYS[d.getDay()],
      dateLabel: `${d.getMonth() + 1}/${d.getDate()}`,
    })
  }
  return result
})

const grid = computed(() => {
  const cells: Cell[] = []
  const counts: number[] = []

  for (const day of days.value) {
    for (let h = 0; h < 24; h++) {
      const key = `${day.iso} ${String(h).padStart(2, '0')}`
      const count = props.data[key] ?? 0
      counts.push(count)
      cells.push({
        key,
        dayLabel: day.dayLabel,
        dateLabel: day.dateLabel,
        hour: h,
        count,
        level: 0,
      })
    }
  }

  // Percentile-based levels
  const nonZero = counts.filter(c => c > 0).sort((a, b) => a - b)
  let thresholds = [0, 0, 0, 0]
  if (nonZero.length > 0) {
    thresholds = [
      1,
      nonZero[Math.floor(nonZero.length * 0.25)] ?? 1,
      nonZero[Math.floor(nonZero.length * 0.50)] ?? 1,
      nonZero[Math.floor(nonZero.length * 0.75)] ?? 1,
    ]
  }

  for (const cell of cells) {
    if (cell.count === 0) cell.level = 0
    else if (cell.count < thresholds[1]) cell.level = 1
    else if (cell.count < thresholds[2]) cell.level = 2
    else if (cell.count < thresholds[3]) cell.level = 3
    else cell.level = 4
  }

  return cells
})

const totalCount = computed(() => grid.value.reduce((sum, c) => sum + c.count, 0))

// ── Tooltip ──

const tooltip = ref<{ x: number; y: number; cell: Cell } | null>(null)

function showTooltip(event: MouseEvent, cell: Cell) {
  const rect = (event.target as HTMLElement).getBoundingClientRect()
  tooltip.value = {
    x: rect.left + rect.width / 2,
    y: rect.top - 8,
    cell,
  }
}

function hideTooltip() {
  tooltip.value = null
}

function formatDate(d: Date): string {
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}

function formatHour(h: number): string {
  return `${String(h).padStart(2, '0')}:00`
}
</script>

<template>
  <div class="heatmap-wrap">
    <!-- Day labels (top) -->
    <div class="heatmap-header">
      <div class="heatmap-hour-gutter"></div>
      <div
        v-for="day in days"
        :key="day.iso"
        class="heatmap-day-label"
      >
        <span class="text-t-fg text-[10px] font-medium">{{ day.dayLabel }}</span>
        <span class="text-t-fg-dark text-[9px]">{{ day.dateLabel }}</span>
      </div>
    </div>

    <!-- Grid: rows = hours, columns = days -->
    <div class="heatmap-body">
      <template v-for="h in 24" :key="h - 1">
        <!-- Hour label -->
        <div class="heatmap-hour-label">
          <span v-if="(h - 1) % 3 === 0" class="text-t-fg-dark text-[9px]">{{ formatHour(h - 1) }}</span>
        </div>
        <!-- Cells for each day at this hour -->
        <div
          v-for="(day, dayIdx) in days"
          :key="`${dayIdx}-${h - 1}`"
          class="heatmap-cell"
          :data-level="grid[dayIdx * 24 + (h - 1)].level"
          :style="{ '--heatmap-color': `var(${colorVar})` }"
          @mouseenter="showTooltip($event, grid[dayIdx * 24 + (h - 1)])"
          @mouseleave="hideTooltip"
        ></div>
      </template>
    </div>

    <!-- Legend -->
    <div class="mt-1.5 flex items-center justify-between">
      <span class="text-t-fg-dark text-[10px]">{{ totalCount.toLocaleString() }} {{ label }} in 7 days</span>
      <div class="flex items-center gap-1">
        <span class="text-t-fg-dark text-[10px]">Less</span>
        <div class="heatmap-cell legend" data-level="0" :style="{ '--heatmap-color': `var(${colorVar})` }"></div>
        <div class="heatmap-cell legend" data-level="1" :style="{ '--heatmap-color': `var(${colorVar})` }"></div>
        <div class="heatmap-cell legend" data-level="2" :style="{ '--heatmap-color': `var(${colorVar})` }"></div>
        <div class="heatmap-cell legend" data-level="3" :style="{ '--heatmap-color': `var(${colorVar})` }"></div>
        <div class="heatmap-cell legend" data-level="4" :style="{ '--heatmap-color': `var(${colorVar})` }"></div>
        <span class="text-t-fg-dark text-[10px]">More</span>
      </div>
    </div>

    <!-- Tooltip -->
    <Teleport to="body">
      <div
        v-if="tooltip"
        class="heatmap-tooltip"
        :style="{ left: tooltip.x + 'px', top: tooltip.y + 'px' }"
      >
        <strong>{{ tooltip.cell.count.toLocaleString() }} {{ label }}</strong>
        <span>{{ tooltip.cell.dayLabel }} {{ tooltip.cell.dateLabel }}, {{ formatHour(tooltip.cell.hour) }}</span>
      </div>
    </Teleport>
  </div>
</template>

<style scoped>
.heatmap-wrap {
  overflow-x: auto;
  overflow-y: hidden;
}

.heatmap-header {
  display: grid;
  grid-template-columns: 32px repeat(7, 11px);
  gap: 2px;
  margin-bottom: 2px;
}

.heatmap-hour-gutter {
  /* empty spacer to align with hour labels */
}

.heatmap-day-label {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 0;
  line-height: 1.1;
}

.heatmap-body {
  display: grid;
  grid-template-columns: 32px repeat(7, 11px);
  gap: 2px;
}

.heatmap-hour-label {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  padding-right: 4px;
  min-height: 12px;
}

.heatmap-cell {
  width: 11px;
  height: 11px;
  border-radius: 2px;
  transition: outline 0.1s;
}

.heatmap-cell:hover {
  outline: 1px solid var(--color-t-fg-dark);
  outline-offset: -1px;
}

.heatmap-cell.legend {
  cursor: default;
  flex-shrink: 0;
  width: 11px;
  height: 11px;
  aspect-ratio: auto;
}

.heatmap-cell.legend:hover {
  outline: none;
}

/* Intensity levels */
.heatmap-cell[data-level="0"] {
  background-color: var(--color-t-bg-highlight);
}

.heatmap-cell[data-level="1"] {
  background-color: color-mix(in srgb, var(--heatmap-color) 25%, var(--color-t-bg-highlight));
}

.heatmap-cell[data-level="2"] {
  background-color: color-mix(in srgb, var(--heatmap-color) 50%, var(--color-t-bg-highlight));
}

.heatmap-cell[data-level="3"] {
  background-color: color-mix(in srgb, var(--heatmap-color) 75%, var(--color-t-bg-highlight));
}

.heatmap-cell[data-level="4"] {
  background-color: var(--heatmap-color);
}
</style>

<style>
/* Global tooltip style (not scoped, since it's teleported) */
.heatmap-tooltip {
  position: fixed;
  transform: translate(-50%, -100%);
  background: var(--color-t-bg-dark);
  border: 1px solid var(--color-t-border);
  border-radius: 4px;
  padding: 4px 8px;
  font-size: 11px;
  font-family: var(--font-mono);
  color: var(--color-t-fg);
  pointer-events: none;
  z-index: 9999;
  white-space: nowrap;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 1px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.3);
}

.heatmap-tooltip span {
  color: var(--color-t-fg-dark);
  font-size: 10px;
}
</style>
